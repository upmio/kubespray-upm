# Worker Node Lifecycle

## Add

只添加下一个连续编号普通Worker，每计划一个，总数不得超过8。

```text
observe/capacity/address checks
-> update planned topology
-> create VM without full provision
-> guest baseline
-> Kubespray facts
-> playbooks/scale.yml
-> Node Ready
-> addon reconciliation
-> verify
```

不得简单执行普通`vagrant up`触发最后节点上的完整Ansible provisioner。

## Remove

只删除当前最高编号普通Worker，每计划一个，总数不得低于3。禁止删除`k8s-1`、`k8s-2`和中间编号节点。

```text
PDB/workload/storage/capacity checks
-> cordon/drain
-> playbooks/remove_node.yml
-> confirm Kubernetes Node absent
-> update inventory/topology
-> internal Vagrant destroy
-> remove owned disks/metadata
-> addon reconciliation
-> verify
```

发现Bound LocalPV、hostPath、未知磁盘数据、不可驱逐Pod、剩余容量不足或VolumeAttachment无法释放时硬拒绝。V1不自动迁移本地数据，不支持失联节点强制删除。

## 成功

Add成功需要VM、Guest、Node、CNI、DaemonSet、labels/taints、Addon和Managed State全部一致。Remove成功需要Node、inventory、VM、domain、受管磁盘和metadata均达到计划状态且其余集群健康。
