# Integration Boundaries

## Legacy迁移来源

`libvirt_kubespray_setup.sh`提供宿主机预检、libvirt/Vagrant/Python准备、网络配置、工作区创建、Vagrant/Kubespray部署和kubeconfig管理。`upm_setup.sh`提供LVM LocalPV、Prometheus、UPM Engine、UPM Platform和Nginx能力。

迁移不是Shell函数逐行翻译。每个行为必须先归入需求和领域用例，再由Go适配器调用底层工具。

## Vagrant

- 所有命令在发现出的部署工作区执行。
- 只读状态使用`vagrant status --machine-readable`。
- libvirt身份优先读取`.vagrant/machines/<node>/libvirt/id`。
- 已有VM start必须带`--no-provision`。
- 不允许任意Vagrant参数透传。

## Kubespray

- 初次部署复用`cluster.yml`。
- Worker添加复用`playbooks/facts.yml`和`playbooks/scale.yml`。
- Worker删除复用`playbooks/remove_node.yml`。
- 不修改或复制Kubespray核心业务逻辑到Go。

## Kubernetes和Helm

- 使用明确kubeconfig执行结构化查询和验收。
- Kubernetes资源存在不等于控制器已运行。
- Helm退出0不等于Addon成功。
- Prometheus安装和验证应使用新的discovery cache，避免陈旧RESTMapper缓存。

## 磁盘

现有脚本扫描全部非根磁盘的行为不进入V1产品契约。Go实现必须使用配置中的磁盘allowlist并在任何初始化前记录签名和归属。
