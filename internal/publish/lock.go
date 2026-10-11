package publish

import "sync"

// 同一集群不能同时跑安装、发布或回滚。锁只在当前进程里有效。
// 开发和生产各自只跑一个控制面进程，所以不需要分布式锁。
var (
	clusterMu sync.Mutex
	clusters  = map[string]*sync.Mutex{}
)

// lockCluster 尝试锁住一个集群。成功时返回解锁函数；已经有任务在跑时返回 false。
func lockCluster(id string) (func(), bool) {
	clusterMu.Lock()
	lock := clusters[id]
	if lock == nil {
		lock = &sync.Mutex{}
		clusters[id] = lock
	}
	clusterMu.Unlock()
	if !lock.TryLock() {
		return nil, false
	}
	return lock.Unlock, true
}
