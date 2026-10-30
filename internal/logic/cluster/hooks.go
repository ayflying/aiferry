package cluster

import (
	"context"
	"sync/atomic"
)

// activeService 指向当前进程启用的集群服务。
//
// 业务逻辑（渠道、系统设置、API Key……）只调用包级 NotifyConfigChanged，
// 不需要把集群服务注入到每个构造函数里；未启用集群时这里保持空指针，
// 调用是零成本的空操作，单机部署行为与现在完全一致。
var activeService atomic.Pointer[Service]

// NotifyConfigChanged 声明某类配置已发生变更，需要同步给子节点。
//
// 只有主节点会真正递增配置版本并推送；子节点收到的是别人的推送，
// 自己再推会形成回环，因此直接忽略。
func NotifyConfigChanged(ctx context.Context, kind, ref string) {
	service := activeService.Load()
	if service == nil || !service.Enabled() || service.IsWorker() {
		return
	}
	if err := service.Mark(ctx, kind, ref); err != nil {
		service.clusterLogger().Warningf(ctx,
			"cluster: mark config change (%s/%s): %v", kind, ref, err)
	}
}
