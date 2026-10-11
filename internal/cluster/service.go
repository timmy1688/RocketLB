// Package cluster 描述一组共用 VIP 的节点。这里只保存期望状态，Keepalived 配置要等发布时才生成。
package cluster

import (
	"context"
	"crypto/rand"
	"rocketlb/internal/httpx"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"rocketlb/internal/model"
	"rocketlb/internal/secret"
	"rocketlb/internal/store"
	"rocketlb/internal/validate"
)

// Member 是界面上看到的集群成员，带节点名字和地址。
type Member struct {
	NodeID   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
	Address  string `json:"address"`
	Priority int    `json:"priority"`
	Role     string `json:"role"`
}

// View 是一个集群。HasAuth 只说明有没有认证串，不会把 Keepalived 密码返回去。
type View struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	VIP       string    `json:"vip"`
	Prefix    int       `json:"prefix"`
	VRID      int       `json:"vrid"`
	Interface string    `json:"interface"`
	AdvertInt int       `json:"advertInt"`
	Layout    string    `json:"layout"`
	HasAuth   bool      `json:"hasAuth"`
	Members   []Member  `json:"members"`
	CreatedAt time.Time `json:"createdAt"`
}

// MemberInput 是保存集群时提交的成员：哪台节点、多大优先级。
type MemberInput struct {
	NodeID   string `json:"nodeId"`
	Priority int    `json:"priority"`
	Role     string `json:"role"`
}

// Write 是创建或修改集群的请求体。AuthPass 留空表示修改时不更换密码。
type Write struct {
	Name      string        `json:"name"`
	VIP       string        `json:"vip"`
	Prefix    int           `json:"prefix"`
	VRID      int           `json:"vrid"`
	Interface string        `json:"interface"`
	AuthPass  string        `json:"authPass"`
	AdvertInt int           `json:"advertInt"`
	Layout    string        `json:"layout"`
	Members   []MemberInput `json:"members"`
}

// Service 保存集群和成员。它不连节点，真正下发在 publish。
type Service struct {
	db  *gorm.DB
	box *secret.Box
}

// NewService 在启动时创建。box 用来加密 Keepalived 认证串。
func NewService(db *gorm.DB, box *secret.Box) *Service {
	return &Service{db: db, box: box}
}

// List 返回全部集群，每个都带上成员。
func (s *Service) List(ctx context.Context) ([]View, error) {
	var rows []model.Cluster
	if err := s.db.WithContext(ctx).Order("name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]View, 0, len(rows))
	for _, row := range rows {
		view, err := s.toView(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

// Get 按编号取一个集群。
func (s *Service) Get(ctx context.Context, id string) (View, error) {
	row, err := s.load(ctx, id)
	if err != nil {
		return View{}, err
	}
	return s.toView(ctx, row)
}

// Create 在一个事务里写入集群和成员。这里只检查成员是否合法；至少两台是发布时的要求。
func (s *Service) Create(ctx context.Context, in Write) (View, error) {
	if err := s.fillCreateDefaults(ctx, &in); err != nil {
		return View{}, err
	}
	if err := validateWrite(in, true); err != nil {
		return View{}, err
	}
	if err := s.checkMembers(ctx, "", in.Members); err != nil {
		return View{}, err
	}
	enc, err := s.box.Encrypt(in.AuthPass)
	if err != nil {
		return View{}, err
	}
	row := model.Cluster{
		ID:            model.NewID(),
		Name:          in.Name,
		VIP:           in.VIP,
		Prefix:        in.Prefix,
		VRID:          in.VRID,
		InterfaceName: in.Interface,
		AuthPassEnc:   enc,
		AdvertInt:     advert(in.AdvertInt),
		Layout:        normalizeLayout(in.Layout),
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return insertMembers(tx, row.ID, in.Members)
	})
	if store.Duplicate(err) {
		return View{}, httpx.Conflict("集群名称已存在")
	}
	if err != nil {
		return View{}, err
	}
	return s.Get(ctx, row.ID)
}

// Update 改集群字段并整体替换成员。用结构体字段名更新，避免 VIP、VRID 被写成错误的列名。
func (s *Service) Update(ctx context.Context, id string, in Write) (View, error) {
	row, err := s.load(ctx, id)
	if err != nil {
		return View{}, err
	}
	if in.Prefix == 0 {
		in.Prefix = row.Prefix
	}
	if in.VRID == 0 {
		in.VRID = row.VRID
	}
	if in.AdvertInt == 0 {
		in.AdvertInt = row.AdvertInt
	}
	if err := s.freezeLiveTopology(ctx, row, in); err != nil {
		return View{}, err
	}
	if err := validateWrite(in, false); err != nil {
		return View{}, err
	}
	if in.AuthPass == "" {
		pass, err := s.box.Decrypt(row.AuthPassEnc)
		if err != nil {
			return View{}, err
		}
		if err := validate.AuthPass(pass); err != nil {
			return View{}, httpx.Invalid(err.Error())
		}
	}
	if err := s.checkMembers(ctx, id, in.Members); err != nil {
		return View{}, err
	}
	next := row
	next.Name = in.Name
	next.VIP = in.VIP
	next.Prefix = in.Prefix
	next.VRID = in.VRID
	next.InterfaceName = in.Interface
	next.AdvertInt = advert(in.AdvertInt)
	next.Layout = normalizeLayout(in.Layout)
	fields := []string{"Name", "VIP", "Prefix", "VRID", "InterfaceName", "AdvertInt", "Layout"}
	if in.AuthPass != "" {
		enc, err := s.box.Encrypt(in.AuthPass)
		if err != nil {
			return View{}, err
		}
		next.AuthPassEnc = enc
		fields = append(fields, "AuthPassEnc")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Cluster{}).Where("id = ?", id).Select(fields).Updates(&next).Error; err != nil {
			return err
		}
		if err := tx.Where("cluster_id = ?", id).Delete(&model.ClusterMember{}).Error; err != nil {
			return err
		}
		return insertMembers(tx, id, in.Members)
	})
	if store.Duplicate(err) {
		return View{}, httpx.Conflict("集群名称已存在")
	}
	if err != nil {
		return View{}, err
	}
	return s.Get(ctx, id)
}

// Delete 连同这个集群的上游、站点、证书、版本和任务一起删掉。节点本身留下，还可以加入别的集群。
func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := s.load(ctx, id); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var upstreams []model.Upstream
		if err := tx.Where("cluster_id = ?", id).Find(&upstreams).Error; err != nil {
			return err
		}
		if len(upstreams) > 0 {
			ids := make([]string, len(upstreams))
			for i, up := range upstreams {
				ids[i] = up.ID
			}
			if err := tx.Where("upstream_id IN ?", ids).Delete(&model.UpstreamServer{}).Error; err != nil {
				return err
			}
		}
		for _, table := range []any{
			&model.DeployJob{},
			&model.Revision{},
			&model.Site{},
			&model.Stream{},
			&model.Certificate{},
			&model.Upstream{},
			&model.ClusterMember{},
		} {
			if err := tx.Where("cluster_id = ?", id).Delete(table).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&model.Cluster{}, "id = ?", id).Error
	})
}

// load 从数据库读一个集群，没有就返回 404。
func (s *Service) load(ctx context.Context, id string) (model.Cluster, error) {
	var row model.Cluster
	err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if err != nil {
		return model.Cluster{}, store.NotFound(err, "集群不存在")
	}
	return row, nil
}

// checkMembers 要求成员存在、地址不同，转发节点的优先级不同，并且没有已经属于别的集群。
func (s *Service) checkMembers(ctx context.Context, clusterID string, members []MemberInput) error {
	seen := map[string]struct{}{}
	prio := map[int]struct{}{}
	addrs := map[string]struct{}{}
	for _, member := range members {
		if member.NodeID == "" {
			return httpx.Invalid("成员缺少节点")
		}
		if _, ok := seen[member.NodeID]; ok {
			return httpx.Invalid("同一个节点不能重复加入集群")
		}
		seen[member.NodeID] = struct{}{}
		if err := validate.Priority(member.Priority); err != nil {
			return httpx.Invalid(err.Error())
		}
		if memberRole(member.Role) != model.MemberNginx {
			if _, ok := prio[member.Priority]; ok {
				return httpx.Invalidf("优先级 %d 重复", member.Priority)
			}
			prio[member.Priority] = struct{}{}
		}
		var node model.Node
		if err := s.db.WithContext(ctx).First(&node, "id = ?", member.NodeID).Error; err != nil {
			return store.NotFound(err, "集群成员中的节点不存在")
		}
		if _, ok := addrs[node.Address]; ok {
			return httpx.Invalid("集群成员的地址不能相同")
		}
		addrs[node.Address] = struct{}{}
		var other int64
		q := s.db.WithContext(ctx).Model(&model.ClusterMember{}).Where("node_id = ?", member.NodeID)
		if clusterID != "" {
			q = q.Where("cluster_id <> ?", clusterID)
		}
		if err := q.Count(&other).Error; err != nil {
			return err
		}
		if other > 0 {
			return httpx.Conflictf("节点 %s 已属于其他集群", node.Name)
		}
	}
	return nil
}

// toView 把集群和成员拼成界面需要的结构。优先级高的排在前面。
func (s *Service) toView(ctx context.Context, row model.Cluster) (View, error) {
	// joined 只在这个函数里用来接成员和节点联表的结果。
	type joined struct {
		NodeID   string
		Priority int
		Role     string
		Name     string
		Address  string
	}
	var rows []joined
	err := s.db.WithContext(ctx).Table("cluster_members").
		Select("cluster_members.node_id, cluster_members.priority, cluster_members.role, nodes.name, nodes.address").
		Joins("join nodes on nodes.id = cluster_members.node_id").
		Where("cluster_members.cluster_id = ?", row.ID).
		Order("cluster_members.priority desc").
		Scan(&rows).Error
	if err != nil {
		return View{}, err
	}
	members := make([]Member, 0, len(rows))
	for _, item := range rows {
		members = append(members, Member{
			NodeID: item.NodeID, NodeName: item.Name, Address: item.Address, Priority: item.Priority,
			Role: memberRole(item.Role),
		})
	}
	pass, err := s.box.Decrypt(row.AuthPassEnc)
	if err != nil {
		return View{}, err
	}
	return View{
		ID:        row.ID,
		Name:      row.Name,
		VIP:       row.VIP,
		Prefix:    row.Prefix,
		VRID:      row.VRID,
		Interface: row.InterfaceName,
		AdvertInt: row.AdvertInt,
		Layout:    normalizeLayout(row.Layout),
		HasAuth:   pass != "",
		Members:   members,
		CreatedAt: row.CreatedAt,
	}, nil
}

// insertMembers 在当前事务里写入成员行。
func insertMembers(tx *gorm.DB, clusterID string, members []MemberInput) error {
	for _, member := range members {
		err := tx.Create(&model.ClusterMember{
			ID:        model.NewID(),
			ClusterID: clusterID,
			NodeID:    member.NodeID,
			Priority:  member.Priority,
			Role:      memberRole(member.Role),
		}).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// validateWrite 检查 VIP、掩码、VRID、网卡和认证串。创建时认证串必填。
func validateWrite(in Write, creating bool) error {
	if err := validate.Name(in.Name); err != nil {
		return httpx.Invalid(err.Error())
	}
	if err := validate.IPv4(in.VIP); err != nil {
		return httpx.Invalid("VIP 需为 IPv4")
	}
	if err := validate.Prefix(in.Prefix); err != nil {
		return httpx.Invalid(err.Error())
	}
	if err := validate.VRID(in.VRID); err != nil {
		return httpx.Invalid(err.Error())
	}
	if err := validate.Interface(in.Interface); err != nil {
		return httpx.Invalid(err.Error())
	}
	if creating || in.AuthPass != "" {
		if err := validate.AuthPass(in.AuthPass); err != nil {
			return httpx.Invalid(err.Error())
		}
	}
	if in.AdvertInt != 0 && (in.AdvertInt < 1 || in.AdvertInt > 10) {
		return httpx.Invalid("通告间隔需在 1 到 10 秒之间")
	}
	return validateLayout(in)
}

// validateLayout 检查部署方案和成员角色是否配套。没选成员时先允许保存方案。
func validateLayout(in Write) error {
	switch normalizeLayout(in.Layout) {
	case model.LayoutCombined:
		for _, member := range in.Members {
			if member.Role != "" && member.Role != model.MemberBoth {
				return httpx.Invalid("两台一起部署时，每台机器都同时转发和跑 nginx")
			}
		}
	case model.LayoutSplit:
		var lvs, nginx int
		for _, member := range in.Members {
			switch member.Role {
			case model.MemberLVS:
				lvs++
			case model.MemberNginx:
				nginx++
			default:
				return httpx.Invalid("分开部署时，每台机器要选「转发」或「nginx」")
			}
		}
		if len(in.Members) > 0 && (lvs < 2 || nginx < 2) {
			return httpx.Invalid("分开部署需要两台转发和两台 nginx")
		}
	default:
		return httpx.Invalid("部署方案无效")
	}
	return nil
}

func normalizeLayout(layout string) string {
	if layout == "" {
		return model.LayoutCombined
	}
	return layout
}

func memberRole(role string) string {
	if role == "" {
		return model.MemberBoth
	}
	return role
}

// fillCreateDefaults 补上用户不必填写的项。前缀默认 24，VRID 从 VIP 末段取并避开已占用的编号，认证串随机生成。
func (s *Service) fillCreateDefaults(ctx context.Context, in *Write) error {
	if in.Prefix == 0 {
		in.Prefix = 24
	}
	if in.AdvertInt == 0 {
		in.AdvertInt = 1
	}
	if in.VRID == 0 {
		vrid, err := s.freeVRID(ctx, vridFromVIP(in.VIP))
		if err != nil {
			return err
		}
		in.VRID = vrid
	}
	if in.AuthPass == "" {
		pass, err := randomAuthPass()
		if err != nil {
			return err
		}
		in.AuthPass = pass
	}
	return nil
}

func vridFromVIP(vip string) int {
	parts := strings.Split(vip, ".")
	if len(parts) != 4 {
		return 51
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil || n < 1 || n > 255 {
		return 51
	}
	return n
}

func (s *Service) freeVRID(ctx context.Context, base int) (int, error) {
	var used []int
	if err := s.db.WithContext(ctx).Model(&model.Cluster{}).Pluck("vrid", &used).Error; err != nil {
		return 0, err
	}
	taken := map[int]bool{}
	for _, n := range used {
		taken[n] = true
	}
	if base < 1 || base > 255 {
		base = 51
	}
	for i := 0; i < 255; i++ {
		n := (base+i-1)%255 + 1
		if !taken[n] {
			return n, nil
		}
	}
	return base, nil
}

func randomAuthPass() (string, error) {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = letters[int(buf[i])%len(letters)]
	}
	return string(buf), nil
}

// freezeLiveTopology 在实例已经发布后拒绝改节点、角色、VIP 和部署方式。
// 这些字段会在下一次发布时改写 Keepalived 和 nginx，直接动到线上流量。名称仍可改。
func (s *Service) freezeLiveTopology(ctx context.Context, row model.Cluster, in Write) error {
	var published int64
	err := s.db.WithContext(ctx).Model(&model.Revision{}).
		Where("cluster_id = ? AND status = ?", row.ID, model.RevisionPublished).
		Count(&published).Error
	if err != nil || published == 0 {
		return err
	}
	current, err := s.toView(ctx, row)
	if err != nil {
		return err
	}
	if topologyChanged(row, current.Members, in) {
		return httpx.Conflict("实例已发布，不能再改节点、角色、VIP 或部署方式")
	}
	return nil
}

func topologyChanged(row model.Cluster, current []Member, in Write) bool {
	if in.VIP != row.VIP || in.Prefix != row.Prefix || in.VRID != row.VRID ||
		in.Interface != row.InterfaceName || advert(in.AdvertInt) != row.AdvertInt ||
		normalizeLayout(in.Layout) != normalizeLayout(row.Layout) || in.AuthPass != "" {
		return true
	}
	return !sameMembers(current, in.Members)
}

func sameMembers(current []Member, next []MemberInput) bool {
	if len(current) != len(next) {
		return false
	}
	got := make(map[string]MemberInput, len(current))
	for _, member := range current {
		got[member.NodeID] = MemberInput{Priority: member.Priority, Role: memberRole(member.Role)}
	}
	for _, member := range next {
		prev, ok := got[member.NodeID]
		if !ok || prev.Priority != member.Priority || prev.Role != memberRole(member.Role) {
			return false
		}
	}
	return true
}

// advert 把没填的 VRRP 通告间隔当成 1 秒。
func advert(n int) int {
	if n == 0 {
		return 1
	}
	return n
}
