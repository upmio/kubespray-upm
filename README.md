# Kubespray UPM

本项目 fork 自 [kubernetes-sigs/kubespray](https://github.com/kubernetes-sigs/kubespray)。除保留上游 Kubespray 能力外，本 fork 的主要改造集中在 [`vagrant_setup_scripts`](vagrant_setup_scripts/README.md)：面向一台 RHEL 系 Linux 主机，使用 libvirt/KVM、Vagrant 和 Kubespray 创建并管理本地多节点 Kubernetes 集群，并可选安装 OpenEBS LVM LocalPV、Prometheus 与 UPM 组件。

> 项目自动化脚本会修改宿主机的虚拟化、网络、安全和用户环境配置。运行前请先阅读 [Libvirt 部署指南](vagrant_setup_scripts/README_libvirt.md)，特别是 firewalld/SELinux、桥接网络和附加磁盘相关警告。

## 本项目快速入口

```bash
cd vagrant_setup_scripts

# 查看实际支持的参数
./libvirt_kubespray_setup.sh --help

# NAT 模式，创建默认 5 节点集群
./libvirt_kubespray_setup.sh -y

# NAT + Cilium，并由 Cilium 替换 kube-proxy
./libvirt_kubespray_setup.sh -y -p cilium \
  --cilium-kube-proxy-replacement

# 桥接模式；网络参数仍需交互输入
./libvirt_kubespray_setup.sh -n bridge
```

- [自动化脚本总览与快速使用](vagrant_setup_scripts/README.md)
- [Libvirt 完整部署、配置和运维指南](vagrant_setup_scripts/README_libvirt.md)
- [上游 Kubespray 文档](https://kubespray.io/)

以下章节介绍本仓库继承的上游 Kubespray 能力；其中通用 Vagrant 流程与本 fork 的 `vagrant_setup_scripts` 自动化流程相互独立。

## Deploy a Production Ready Kubernetes Cluster

![Kubernetes Logo](https://raw.githubusercontent.com/kubernetes-sigs/kubespray/master/docs/img/kubernetes-logo.png)

If you have questions, check the documentation at [kubespray.io](https://kubespray.io) and join us on the [kubernetes slack](https://kubernetes.slack.com), channel **\#kubespray**.
You can get your invite [here](http://slack.k8s.io/)

- Can be deployed on **[AWS](docs/cloud_providers/aws.md), GCE, [Azure](docs/cloud_providers/azure.md), [OpenStack](docs/cloud_controllers/openstack.md), [vSphere](docs/cloud_controllers/vsphere.md), Equinix Metal (bare metal), Oracle Cloud Infrastructure (Experimental), or Baremetal**
- **Highly available** cluster
- **Composable** (Choice of the network plugin for instance)
- Supports most popular **Linux distributions**
- **Continuous integration tests**

## Quick Start

Below are several ways to use Kubespray to deploy a Kubernetes cluster.

### Docker

Ensure you have installed Docker then

```ShellSession
docker run --rm -it --mount type=bind,source="$(pwd)"/inventory/sample,dst=/inventory \
  --mount type=bind,source="${HOME}"/.ssh/id_rsa,dst=/root/.ssh/id_rsa \
  quay.io/kubespray/kubespray:v2.31.0 bash
# Inside the container you may now run the kubespray playbooks:
ansible-playbook -i /inventory/inventory.ini --private-key /root/.ssh/id_rsa cluster.yml
```

### Ansible

#### Usage

See [Getting started](docs/getting_started/getting-started.md)

#### Collection

See [here](docs/ansible/ansible_collection.md) if you wish to use this repository as an Ansible collection

### Upstream Vagrant workflow

This is the upstream generic Vagrant workflow. For this fork's Linux/libvirt automation, use [`vagrant_setup_scripts`](vagrant_setup_scripts/README.md) instead.

For Vagrant we need to install Python dependencies for provisioning tasks.
Check that ``Python`` and ``pip`` are installed:

```ShellSession
python -V && pip -V
```

If this returns the version of the software, you're good to go. If not, download and install Python from here <https://www.python.org/downloads/source/>

Install Ansible according to [Ansible installation guide](docs/ansible/ansible.md#installing-ansible)
then run the following step:

```ShellSession
vagrant up
```

## Documents

- [Requirements](#requirements)
- [Kubespray vs ...](docs/getting_started/comparisons.md)
- [Getting started](docs/getting_started/getting-started.md)
- [Setting up your first cluster](docs/getting_started/setting-up-your-first-cluster.md)
- [Ansible inventory and tags](docs/ansible/ansible.md)
- [Integration with existing ansible repo](docs/operations/integration.md)
- [Deployment data variables](docs/ansible/vars.md)
- [DNS stack](docs/advanced/dns-stack.md)
- [HA mode](docs/operations/ha-mode.md)
- [Network plugins](#network-plugins)
- [Vagrant install](docs/developers/vagrant.md)
- [Flatcar Container Linux bootstrap](docs/operating_systems/flatcar.md)
- [Fedora CoreOS bootstrap](docs/operating_systems/fcos.md)
- [openSUSE setup](docs/operating_systems/opensuse.md)
- [Downloaded artifacts](docs/advanced/downloads.md)
- Equinix Metal
- [OpenStack](docs/cloud_controllers/openstack.md)
- [vSphere](docs/cloud_controllers/vsphere.md)
- [Large deployments](docs/operations/large-deployments.md)
- [Adding/replacing a node](docs/operations/nodes.md)
- [Upgrades basics](docs/operations/upgrades.md)
- [Air-Gap installation](docs/operations/offline-environment.md)
- [NTP](docs/advanced/ntp.md)
- [Hardening](docs/operations/hardening.md)
- [Mirror](docs/operations/mirror.md)
- [Roadmap](docs/roadmap/roadmap.md)

## Supported Linux Distributions

- **Flatcar Container Linux by Kinvolk**
- **Debian** Bookworm, Bullseye, Trixie
- **Ubuntu** 22.04, 24.04
- **CentOS Stream / RHEL** 9, 10
- **Fedora** 39, 40, 41, 42
- **Fedora CoreOS** (see [fcos Note](docs/operating_systems/fcos.md))
- **openSUSE** Leap 15.x/Tumbleweed
- **Oracle Linux** 9, 10
- **Alma Linux** 9, 10
- **Rocky Linux** 9, 10 (experimental in 10: see [Rocky Linux 10 notes](docs/operating_systems/rhel.md#rocky-linux-10))
- **Kylin Linux Advanced Server V10** (experimental: see [kylin linux notes](docs/operating_systems/kylinlinux.md))
- **Amazon Linux 2** (experimental: see [amazon linux notes](docs/operating_systems/amazonlinux.md))
- **UOS Linux** (experimental: see [uos linux notes](docs/operating_systems/uoslinux.md))
- **openEuler** (experimental: see [openEuler notes](docs/operating_systems/openeuler.md))

Note:

- Upstart/SysV init based OS types are not supported.
- [Kernel requirements](docs/operations/kernel-requirements.md) (please read if the OS kernel version is < 4.19).

## Supported Components

<!-- BEGIN ANSIBLE MANAGED BLOCK -->

- Core
  - [kubernetes](https://github.com/kubernetes/kubernetes) 1.36.2
  - [etcd](https://github.com/etcd-io/etcd) 3.6.12
  - [docker](https://www.docker.com/) 28.3
  - [containerd](https://containerd.io/) 2.3.1
  - [cri-o](https://cri-o.io/) 1.36.1 (experimental: see [CRI-O Note](docs/CRI/cri-o.md). Only on fedora, ubuntu and centos based OS)
- Network Plugin
  - [cni-plugins](https://github.com/containernetworking/plugins) 1.9.1
  - [calico](https://github.com/projectcalico/calico) 3.31.5
  - [cilium](https://github.com/cilium/cilium) 1.19.5
  - [flannel](https://github.com/flannel-io/flannel) 0.28.4
  - [kube-ovn](https://github.com/alauda/kube-ovn) 1.12.21
  - [kube-router](https://github.com/cloudnativelabs/kube-router) 2.1.1
  - [multus](https://github.com/k8snetworkplumbingwg/multus-cni) 4.2.2
  - [kube-vip](https://github.com/kube-vip/kube-vip) 1.0.3
- Application
  - [cert-manager](https://github.com/jetstack/cert-manager) 1.15.3
  - [coredns](https://github.com/coredns/coredns) 1.14.2
  - [argocd](https://argoproj.github.io/) 2.14.5
  - [helm](https://helm.sh/) 3.18.4
  - [metallb](https://metallb.universe.tf/) 0.13.9
  - [registry](https://github.com/distribution/distribution) 2.8.1
- Storage Plugin
  - [aws-ebs-csi-plugin](https://github.com/kubernetes-sigs/aws-ebs-csi-driver) 0.5.0
  - [azure-csi-plugin](https://github.com/kubernetes-sigs/azuredisk-csi-driver) 1.10.0
  - [cinder-csi-plugin](https://github.com/kubernetes/cloud-provider-openstack/blob/master/docs/cinder-csi-plugin/using-cinder-csi-plugin.md) 1.30.0
  - [gcp-pd-csi-plugin](https://github.com/kubernetes-sigs/gcp-compute-persistent-disk-csi-driver) 1.9.2
  - [local-path-provisioner](https://github.com/rancher/local-path-provisioner) 0.0.32
  - [local-volume-provisioner](https://github.com/kubernetes-sigs/sig-storage-local-static-provisioner) 2.5.0
  - [node-feature-discovery](https://github.com/kubernetes-sigs/node-feature-discovery) 0.16.4

<!-- END ANSIBLE MANAGED BLOCK -->

## Container Runtime Notes

- The cri-o version should be aligned with the respective kubernetes version (i.e. kube_version=1.20.x, crio_version=1.20)

## Requirements

- **Minimum required version of Kubernetes is v1.30**
- **Install the repository-pinned Ansible dependencies from `requirements.txt`; the current compatibility range is ansible-core >=2.18,<2.19 with Python 3.11-3.13**
- The target servers must have **access to the Internet** in order to pull docker images. Otherwise, additional configuration is required (See [Offline Environment](docs/operations/offline-environment.md))
- The target servers are configured to allow **IPv4 forwarding**.
- If using IPv6 for pods and services, the target servers are configured to allow **IPv6 forwarding**.
- The **firewalls are not managed**, you'll need to implement your own rules the way you used to.
    in order to avoid any issue during deployment you should disable your firewall.
- If kubespray is run from non-root user account, correct privilege escalation method
    should be configured in the target servers. Then the `ansible_become` flag
    or command parameters `--become or -b` should be specified.

Hardware:
These limits are safeguarded by Kubespray. Actual requirements for your workload can differ. For a sizing guide go to the [Building Large Clusters](https://kubernetes.io/docs/setup/cluster-large/#size-of-master-and-master-components) guide.

- Control Plane
  - Memory: 2 GB
- Worker Node
  - Memory: 1 GB

## Network Plugins

You can choose among ten network plugins. The upstream generic Vagrant workflow may use `flannel`; this fork's `vagrant_setup_scripts` automation supports Calico and Cilium, with Calico as the default.

- [flannel](docs/CNI/flannel.md): gre/vxlan (layer 2) networking.

- [Calico](https://docs.tigera.io/calico/latest/about/) is a networking and network policy provider. Calico supports a flexible set of networking options
    designed to give you the most efficient networking across a range of situations, including non-overlay
    and overlay networks, with or without BGP. Calico uses the same engine to enforce network policy for hosts,
    pods, and (if using Istio and Envoy) applications at the service mesh layer.

- [cilium](http://docs.cilium.io/en/latest/): layer 3/4 networking (as well as layer 7 to protect and secure application protocols), supports dynamic insertion of BPF bytecode into the Linux kernel to implement security services, networking and visibility logic.

- [kube-ovn](docs/CNI/kube-ovn.md): Kube-OVN integrates the OVN-based Network Virtualization with Kubernetes. It offers an advanced Container Network Fabric for Enterprises.

- [kube-router](docs/CNI/kube-router.md): Kube-router is a L3 CNI for Kubernetes networking aiming to provide operational
    simplicity and high performance: it uses IPVS to provide Kube Services Proxy (if setup to replace kube-proxy),
    iptables for network policies, and BGP for ods L3 networking (with optionally BGP peering with out-of-cluster BGP peers).
    It can also optionally advertise routes to Kubernetes cluster Pods CIDRs, ClusterIPs, ExternalIPs and LoadBalancerIPs.

- [macvlan](docs/CNI/macvlan.md): Macvlan is a Linux network driver. Pods have their own unique Mac and Ip address, connected directly the physical (layer 2) network.

- [multus](docs/CNI/multus.md): Multus is a meta CNI plugin that provides multiple network interface support to pods. For each interface Multus delegates CNI calls to secondary CNI plugins such as Calico, macvlan, etc.

- `custom_cni`: You can specify manifests that will be applied to the cluster to bring your own CNI and use non-supported ones.
  See `tests/files/custom_cni/README.md` and `tests/files/custom_cni/values.yaml` for an example with a CNI provided by a Helm Chart.

The network plugin to use is defined by the variable `kube_network_plugin`. There is also an
option to leverage built-in cloud provider networking instead.
See also [Network checker](docs/advanced/netcheck.md).

## Ingress Plugins

- [metallb](docs/ingress/metallb.md): the MetalLB bare-metal service LoadBalancer provider.

## Community docs and resources

- [kubernetes.io/docs/setup/production-environment/tools/kubespray/](https://kubernetes.io/docs/setup/production-environment/tools/kubespray/)
- [kubespray, monitoring and logging](https://github.com/gregbkr/kubernetes-kargo-logging-monitoring) by @gregbkr
- [Deploy Kubernetes w/ Ansible & Terraform](https://rsmitty.github.io/Terraform-Ansible-Kubernetes/) by @rsmitty
- [Deploy a Kubernetes Cluster with Kubespray (video)](https://www.youtube.com/watch?v=CJ5G4GpqDy0)

## Tools and projects on top of Kubespray

- [Digital Rebar Provision](https://github.com/digitalrebar/provision/blob/v4/doc/integrations/ansible.rst)
- [Terraform Contrib](https://github.com/kubernetes-sigs/kubespray/tree/master/contrib/terraform)
- [Kubean](https://github.com/kubean-io/kubean)

## CI Tests

[![Build graphs](https://gitlab.com/kargo-ci/kubernetes-sigs-kubespray/badges/master/pipeline.svg)](https://gitlab.com/kargo-ci/kubernetes-sigs-kubespray/-/pipelines)

CI/end-to-end tests sponsored by: [CNCF](https://cncf.io), [Equinix Metal](https://metal.equinix.com/), [OVHcloud](https://www.ovhcloud.com/), [ELASTX](https://elastx.se/).

See the [test matrix](docs/developers/test_cases.md) for details.
