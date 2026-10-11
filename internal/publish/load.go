// 本文件从数据库组装渲染输入。证书私钥和 Keepalived 认证串在这里解密，只留在内存里。
package publish

import (
	"context"
	"rocketlb/internal/httpx"
	"sort"

	"gorm.io/gorm"

	"rocketlb/internal/model"
	"rocketlb/internal/render"
	"rocketlb/internal/store"
)

// Bundle 从数据库读出集群、成员、上游、站点和证书，交给 render.Build。预览、发布和安装前都会走这里。
func (s *Service) Bundle(ctx context.Context, clusterID string) (render.Bundle, error) {
	in, err := s.input(ctx, clusterID)
	if err != nil {
		return render.Bundle{}, err
	}
	bundle, err := render.Build(in)
	if err != nil {
		return render.Bundle{}, httpx.Invalid(err.Error())
	}
	return bundle, nil
}

// input 把数据库行收成渲染器认识的 Input，并在这里解密认证串和证书私钥。
func (s *Service) input(ctx context.Context, clusterID string) (render.Input, error) {
	var cluster model.Cluster
	if err := s.db.WithContext(ctx).First(&cluster, "id = ?", clusterID).Error; err != nil {
		return render.Input{}, store.NotFound(err, "集群不存在")
	}
	pass, err := s.box.Decrypt(cluster.AuthPassEnc)
	if err != nil {
		return render.Input{}, err
	}
	nodes, err := s.Nodes(ctx, clusterID)
	if err != nil {
		return render.Input{}, err
	}
	members := make([]render.Member, 0, len(nodes))
	for _, node := range nodes {
		members = append(members, render.Member{
			NodeID:   node.ID,
			NodeName: node.Name,
			Address:  node.Address,
			Priority: node.Priority,
			Role:     node.Role,
		})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Address < members[j].Address })

	var upstreams []model.Upstream
	err = s.db.WithContext(ctx).Preload("Servers", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort asc")
	}).Where("cluster_id = ?", clusterID).Order("name asc").Find(&upstreams).Error
	if err != nil {
		return render.Input{}, err
	}
	upByID := map[string]model.Upstream{}
	renderUps := make([]render.Upstream, 0, len(upstreams))
	for _, up := range upstreams {
		upByID[up.ID] = up
		item := render.Upstream{Name: up.Name, Layer: up.Layer, Method: up.Method}
		for _, server := range up.Servers {
			item.Servers = append(item.Servers, render.Server{
				Address: server.Address, Port: server.Port, Weight: server.Weight,
				MaxFails: server.MaxFails, FailTimeout: server.FailTimeout,
				Backup: server.Backup, Down: server.Down,
			})
		}
		renderUps = append(renderUps, item)
	}

	var sites []model.Site
	if err := s.db.WithContext(ctx).Where("cluster_id = ?", clusterID).Order("name asc").Find(&sites).Error; err != nil {
		return render.Input{}, err
	}
	var streams []model.Stream
	if err := s.db.WithContext(ctx).Where("cluster_id = ?", clusterID).Order("name asc").Find(&streams).Error; err != nil {
		return render.Input{}, err
	}
	var certs []model.Certificate
	if err := s.db.WithContext(ctx).Where("cluster_id = ?", clusterID).Find(&certs).Error; err != nil {
		return render.Input{}, err
	}
	certByID := map[string]model.Certificate{}
	for _, cert := range certs {
		certByID[cert.ID] = cert
	}

	renderSites := make([]render.Site, 0, len(sites))
	usedCerts := map[string]model.Certificate{}
	for _, site := range sites {
		up, ok := upByID[site.UpstreamID]
		if !ok {
			return render.Input{}, httpx.Invalidf("站点 %s 的上游不存在", site.Name)
		}
		item := render.Site{
			Name: site.Name, ListenPort: site.ListenPort, ServerName: site.ServerName,
			TLS: site.TLS, Upstream: up.Name, WebSocket: site.WebSocket,
		}
		if site.TLS {
			cert, ok := certByID[site.CertificateID]
			if !ok {
				return render.Input{}, httpx.Invalidf("站点 %s 的证书不存在", site.Name)
			}
			item.CertName = cert.Name
			usedCerts[cert.ID] = cert
		}
		renderSites = append(renderSites, item)
	}
	renderStreams := make([]render.Stream, 0, len(streams))
	for _, stream := range streams {
		up, ok := upByID[stream.UpstreamID]
		if !ok {
			return render.Input{}, httpx.Invalidf("四层代理 %s 的上游不存在", stream.Name)
		}
		renderStreams = append(renderStreams, render.Stream{
			Name: stream.Name, Protocol: stream.Protocol, ListenPort: stream.ListenPort, Upstream: up.Name,
		})
	}
	renderCerts := make([]render.Certificate, 0, len(usedCerts))
	for _, cert := range usedCerts {
		body, err := s.box.Decrypt(cert.CertEnc)
		if err != nil {
			return render.Input{}, err
		}
		key, err := s.box.Decrypt(cert.KeyEnc)
		if err != nil {
			return render.Input{}, err
		}
		renderCerts = append(renderCerts, render.Certificate{Name: cert.Name, Certificate: body, PrivateKey: key})
	}
	sort.Slice(renderCerts, func(i, j int) bool { return renderCerts[i].Name < renderCerts[j].Name })

	return render.Input{
		Cluster: render.Cluster{
			Name: cluster.Name, VIP: cluster.VIP, Prefix: cluster.Prefix, VRID: cluster.VRID,
			Interface: cluster.InterfaceName, AuthPass: pass, AdvertInt: cluster.AdvertInt,
		},
		Members:      members,
		Upstreams:    renderUps,
		Sites:        renderSites,
		Streams:      renderStreams,
		Certificates: renderCerts,
	}, nil
}
