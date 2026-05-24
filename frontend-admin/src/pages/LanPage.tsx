import { DeleteOutlined, DownloadOutlined, EditOutlined, ExperimentOutlined, PlusOutlined, ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Form, Input, InputNumber, message, Popconfirm, Select, Space, Switch, Table, Tabs, Tag, Tooltip, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { useEffect, useMemo, useState } from 'react'
import { listDevices } from '../api/devices'
import {
  createVirtualDeviceGroup,
  createVirtualNetwork,
  createVirtualACLRule,
  createVirtualRoute,
  deleteVirtualACLRule,
  deleteVirtualDeviceGroup,
  deleteVirtualNetwork,
  deleteVirtualRoute,
  downloadLANDiagnostics,
  listVirtualACLRules,
  listVirtualAddresses,
  listVirtualDeviceKeys,
  listVirtualDeviceGroups,
  listVirtualDeviceStates,
  listVirtualNetworks,
  listVirtualPeerPathEvents,
  listVirtualPeerStates,
  listVirtualRoutes,
  reassignVirtualAddress,
  releaseVirtualAddress,
  triggerVirtualAddressBootstrap,
  updateVirtualACLRule,
  updateVirtualAddress,
  updateVirtualDeviceGroup,
  updateVirtualNetwork,
  updateVirtualRoute,
} from '../api/lan'
import type { Device, VirtualACLRule, VirtualACLRulePayload, VirtualAddress, VirtualDeviceGroup, VirtualDeviceGroupPayload, VirtualNetwork, VirtualRoute, VirtualRoutePayload } from '../types/api'

type NetworkForm = Pick<VirtualNetwork, 'name' | 'cidr' | 'mtu' | 'mss' | 'path_policy' | 'enabled'>
type AddressForm = Pick<VirtualAddress, 'network_id' | 'virtual_ip' | 'hostname' | 'dns_enabled'>
type ACLForm = VirtualACLRulePayload
type RouteForm = VirtualRoutePayload
type GroupForm = VirtualDeviceGroupPayload

export function LanPage() {
  const queryClient = useQueryClient()
  const [networkForm] = Form.useForm<NetworkForm>()
  const [addressForm] = Form.useForm<AddressForm>()
  const [aclForm] = Form.useForm<ACLForm>()
  const [routeForm] = Form.useForm<RouteForm>()
  const [groupForm] = Form.useForm<GroupForm>()
  const [editingAddress, setEditingAddress] = useState<VirtualAddress | null>(null)
  const [editingACL, setEditingACL] = useState<VirtualACLRule | null>(null)
  const [aclFormOpen, setACLFormOpen] = useState(false)
  const [editingRoute, setEditingRoute] = useState<VirtualRoute | null>(null)
  const [routeFormOpen, setRouteFormOpen] = useState(false)
  const [editingGroup, setEditingGroup] = useState<VirtualDeviceGroup | null>(null)
  const [groupFormOpen, setGroupFormOpen] = useState(false)
  const [selectedNetworkID, setSelectedNetworkID] = useState<number | undefined>()
  const [creatingNetwork, setCreatingNetwork] = useState(false)

  const networks = useQuery({ queryKey: ['lan', 'networks'], queryFn: listVirtualNetworks })
  const currentNetwork = useMemo(() => {
    const items = networks.data || []
    return items.find((network) => network.id === selectedNetworkID) || items[0]
  }, [networks.data, selectedNetworkID])
  const networkID = currentNetwork?.id
  const addresses = useQuery({
    queryKey: ['lan', 'addresses', networkID],
    queryFn: () => listVirtualAddresses(networkID),
    enabled: Boolean(networkID),
  })
  const acl = useQuery({
    queryKey: ['lan', 'acl', networkID],
    queryFn: () => listVirtualACLRules(networkID),
    enabled: Boolean(networkID),
  })
  const states = useQuery({
    queryKey: ['lan', 'peer-states', networkID],
    queryFn: () => listVirtualPeerStates(networkID),
    enabled: Boolean(networkID),
    refetchInterval: 10000,
  })
  const pathEvents = useQuery({
    queryKey: ['lan', 'path-events', networkID],
    queryFn: () => listVirtualPeerPathEvents(networkID),
    enabled: Boolean(networkID),
    refetchInterval: 10000,
  })
  const deviceStates = useQuery({
    queryKey: ['lan', 'device-states', networkID],
    queryFn: () => listVirtualDeviceStates(networkID),
    enabled: Boolean(networkID),
    refetchInterval: 10000,
  })
  const routes = useQuery({
    queryKey: ['lan', 'routes', networkID],
    queryFn: () => listVirtualRoutes(networkID),
    enabled: Boolean(networkID),
  })
  const devices = useQuery({ queryKey: ['devices'], queryFn: listDevices })
  const deviceKeys = useQuery({ queryKey: ['lan', 'device-keys'], queryFn: listVirtualDeviceKeys })
  const groups = useQuery({ queryKey: ['lan', 'groups'], queryFn: listVirtualDeviceGroups })

  useEffect(() => {
    if (!selectedNetworkID && networks.data?.[0]) {
      setSelectedNetworkID(networks.data[0].id)
    }
  }, [networks.data, selectedNetworkID])

  useEffect(() => {
    if (currentNetwork) {
      networkForm.setFieldsValue({
        name: currentNetwork.name,
        cidr: currentNetwork.cidr,
        mtu: currentNetwork.mtu || 0,
        mss: currentNetwork.mss || 0,
        path_policy: currentNetwork.path_policy || 'prefer_p2p',
        enabled: currentNetwork.enabled,
      })
    }
  }, [currentNetwork, networkForm])

  useEffect(() => {
    if (editingAddress) {
      addressForm.setFieldsValue({
        network_id: editingAddress.network_id,
        virtual_ip: editingAddress.virtual_ip,
        hostname: editingAddress.hostname,
        dns_enabled: editingAddress.dns_enabled,
      })
    }
  }, [addressForm, editingAddress])

  useEffect(() => {
    if (editingACL) {
      aclForm.setFieldsValue(normalizeACLForm(editingACL))
      setACLFormOpen(true)
    }
  }, [aclForm, editingACL])
  useEffect(() => {
    if (editingRoute) {
      routeForm.setFieldsValue(normalizeRouteForm(editingRoute))
      setRouteFormOpen(true)
    }
  }, [editingRoute, routeForm])
  useEffect(() => {
    if (editingGroup) {
      groupForm.setFieldsValue({
        id: editingGroup.id,
        name: editingGroup.name,
        device_ids: editingGroup.device_ids || [],
      })
      setGroupFormOpen(true)
    }
  }, [editingGroup, groupForm])

  const deviceOptions = useMemo(() => (devices.data || []).map((device) => ({
    label: device.name ? `${device.name} (${device.id})` : device.id,
    value: device.id,
  })), [devices.data])
  const groupOptions = useMemo(() => (groups.data || []).map((group) => ({
    label: group.name ? `${group.name} (${group.id})` : group.id,
    value: group.id,
  })), [groups.data])
  const groupName = useMemo(() => {
    const m = new Map<string, VirtualDeviceGroup>()
    for (const group of groups.data || []) {
      m.set(group.id, group)
    }
    return (id?: string) => {
      if (!id) return '-'
      const group = m.get(id)
      return group?.name ? `${group.name} (${id})` : id
    }
  }, [groups.data])
  const deviceName = useMemo(() => {
    const m = new Map<string, Device>()
    for (const device of devices.data || []) {
      m.set(device.id, device)
    }
    return (id?: string) => {
      if (!id) return '-'
      const device = m.get(id)
      return device?.name ? `${device.name} (${id})` : id
    }
  }, [devices.data])
  const keyByDevice = useMemo(() => {
    const m = new Map<string, { algorithm: string; public_key: string; updated_at: string }>()
    for (const key of deviceKeys.data || []) {
      m.set(key.device_id, key)
    }
    return m
  }, [deviceKeys.data])

  const refreshLAN = () => {
    queryClient.invalidateQueries({ queryKey: ['lan'] })
  }
  const downloadDiagnostics = async () => {
    try {
      const data = await downloadLANDiagnostics(networkID)
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json;charset=utf-8' })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `udptunnellan-diagnostics-${dayjs().format('YYYYMMDD-HHmmss')}.json`
      document.body.appendChild(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(url)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '诊断导出失败')
    }
  }
  const networkMutation = useMutation<VirtualNetwork | { ok: boolean }, Error, NetworkForm>({
    mutationFn: (payload: NetworkForm) => creatingNetwork || !networkID ? createVirtualNetwork(payload) : updateVirtualNetwork(networkID, payload),
    onSuccess: (network) => {
      message.success(creatingNetwork ? '虚拟网络已创建' : '虚拟网络已保存')
      if (creatingNetwork && network && 'id' in network) {
        setSelectedNetworkID(network.id)
      }
      setCreatingNetwork(false)
      refreshLAN()
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '保存失败'),
  })
  const deleteNetworkMutation = useMutation({
    mutationFn: deleteVirtualNetwork,
    onSuccess: () => {
      message.success('虚拟网络已删除')
      setSelectedNetworkID(undefined)
      setCreatingNetwork(false)
      networkForm.resetFields()
      refreshLAN()
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '删除失败'),
  })
  const addressMutation = useMutation({
    mutationFn: ({ deviceID, payload }: { deviceID: string; payload: AddressForm }) => updateVirtualAddress(deviceID, payload),
    onSuccess: () => {
      message.success('虚拟 IP 已保存')
      setEditingAddress(null)
      addressForm.resetFields()
      queryClient.invalidateQueries({ queryKey: ['lan', 'addresses'] })
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '保存失败'),
  })
  const addressActionMutation = useMutation({
    mutationFn: ({ action, row }: { action: 'release' | 'reassign' | 'bootstrap'; row: VirtualAddress }) => {
      if (action === 'release') return releaseVirtualAddress(row.device_id, row.network_id)
      if (action === 'reassign') return reassignVirtualAddress(row.device_id, row.network_id)
      return triggerVirtualAddressBootstrap(row.device_id, row.network_id)
    },
    onSuccess: (_, vars) => {
      const label = vars.action === 'release' ? '虚拟 IP 已释放' : vars.action === 'reassign' ? '虚拟 IP 已重新分配' : '已触发重新 bootstrap'
      message.success(label)
      queryClient.invalidateQueries({ queryKey: ['lan'] })
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '操作失败'),
  })
  const aclMutation = useMutation<unknown, Error, ACLForm>({
    mutationFn: (payload: ACLForm) => editingACL ? updateVirtualACLRule(editingACL.id, payload) : createVirtualACLRule(payload),
    onSuccess: () => {
      message.success('ACL 规则已保存')
      setEditingACL(null)
      setACLFormOpen(false)
      aclForm.resetFields()
      queryClient.invalidateQueries({ queryKey: ['lan', 'acl'] })
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '保存失败'),
  })
  const deleteACLMutation = useMutation({
    mutationFn: deleteVirtualACLRule,
    onSuccess: () => {
      message.success('ACL 规则已删除')
      queryClient.invalidateQueries({ queryKey: ['lan', 'acl'] })
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '删除失败'),
  })
  const routeMutation = useMutation<unknown, Error, RouteForm>({
    mutationFn: (payload: RouteForm) => editingRoute ? updateVirtualRoute(editingRoute.id, payload) : createVirtualRoute(payload),
    onSuccess: () => {
      message.success('虚拟路由已保存')
      setEditingRoute(null)
      setRouteFormOpen(false)
      routeForm.resetFields()
      queryClient.invalidateQueries({ queryKey: ['lan', 'routes'] })
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '保存失败'),
  })
  const deleteRouteMutation = useMutation({
    mutationFn: deleteVirtualRoute,
    onSuccess: () => {
      message.success('虚拟路由已删除')
      queryClient.invalidateQueries({ queryKey: ['lan', 'routes'] })
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '删除失败'),
  })
  const groupMutation = useMutation<unknown, Error, GroupForm>({
    mutationFn: (payload: GroupForm) => editingGroup ? updateVirtualDeviceGroup(editingGroup.id, payload) : createVirtualDeviceGroup(payload),
    onSuccess: () => {
      message.success('设备组已保存')
      setEditingGroup(null)
      setGroupFormOpen(false)
      groupForm.resetFields()
      queryClient.invalidateQueries({ queryKey: ['lan', 'groups'] })
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '保存失败'),
  })
  const deleteGroupMutation = useMutation({
    mutationFn: deleteVirtualDeviceGroup,
    onSuccess: () => {
      message.success('设备组已删除')
      queryClient.invalidateQueries({ queryKey: ['lan', 'groups'] })
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '删除失败'),
  })

  const tabs = [
    {
      key: 'network',
      label: '网络配置',
      children: (
        <Card>
          <Alert
            className="lan-alert"
            type="info"
            showIcon
            icon={<ExperimentOutlined />}
            message="虚拟局域网为实验功能"
            description="网络归属由服务端后台配置，UDPTunnelLAN 客户端不需要本地选择网络。"
          />
          <div className="lan-section-toolbar">
            <Select
              value={networkID}
              loading={networks.isLoading}
              options={(networks.data || []).map((network) => ({ value: network.id, label: `${network.name} (${network.cidr})` }))}
              onChange={(id) => { setSelectedNetworkID(id); setCreatingNetwork(false) }}
              placeholder="选择虚拟网络"
              className="lan-network-select"
            />
            <Space>
              <Button
                icon={<PlusOutlined />}
                onClick={() => {
                  setCreatingNetwork(true)
                  setSelectedNetworkID(undefined)
                  networkForm.setFieldsValue({ name: '', cidr: '172.16.10.0/24', mtu: 0, mss: 0, path_policy: 'prefer_p2p', enabled: true })
                }}
              >
                新增网络
              </Button>
              {currentNetwork && !creatingNetwork ? (
                <Popconfirm
                  title="删除虚拟网络"
                  description="仅空网络可删除。请先处理地址、ACL、路由和状态记录。"
                  onConfirm={() => deleteNetworkMutation.mutate(currentNetwork.id)}
                >
                  <Button danger icon={<DeleteOutlined />} loading={deleteNetworkMutation.isPending}>删除网络</Button>
                </Popconfirm>
              ) : null}
            </Space>
          </div>
          <Form form={networkForm} layout="vertical" className="lan-form" onFinish={(values) => networkMutation.mutate(values)}>
            <Form.Item name="name" label="网络名称" rules={[{ required: true, message: '请输入网络名称' }]}>
              <Input placeholder="默认虚拟网络" />
            </Form.Item>
            <Form.Item
              name="cidr"
              label="默认网段"
              rules={[{ required: true, message: '请输入 CIDR' }, { pattern: /^\d{1,3}(\.\d{1,3}){3}\/\d{1,2}$/, message: '请输入 CIDR，例如 172.16.10.0/24' }]}
            >
              <Input placeholder="172.16.10.0/24" />
            </Form.Item>
            <Space size="large" align="start">
              <Form.Item name="mtu" label="MTU" tooltip="0 表示使用客户端默认值 1280">
                <InputNumber min={0} max={9000} placeholder="0" />
              </Form.Item>
              <Form.Item name="mss" label="MSS" tooltip="0 表示按 MTU 自动计算">
                <InputNumber min={0} max={1200} placeholder="0" />
              </Form.Item>
            </Space>
            <Form.Item name="path_policy" label="路径策略" initialValue="prefer_p2p">
              <Select
                options={[
                  { value: 'prefer_p2p', label: '优先 P2P' },
                  { value: 'auto', label: '自动' },
                  { value: 'prefer_relay', label: '优先 Relay' },
                  { value: 'relay_only', label: '仅 Relay' },
                ]}
              />
            </Form.Item>
            <Form.Item name="enabled" label="启用 LAN" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={networkMutation.isPending} disabled={!creatingNetwork && !currentNetwork}>
              {creatingNetwork ? '创建网络' : '保存网络配置'}
            </Button>
            {creatingNetwork ? <Button onClick={() => { setCreatingNetwork(false); setSelectedNetworkID(networks.data?.[0]?.id); networkForm.resetFields() }}>取消</Button> : null}
          </Form>
        </Card>
      ),
    },
    {
      key: 'addresses',
      label: '设备虚拟 IP',
      children: (
        <Card>
          {editingAddress && (
            <Form form={addressForm} layout="vertical" className="lan-address-form" onFinish={(values) => addressMutation.mutate({ deviceID: editingAddress.device_id, payload: values })}>
              <Form.Item label="设备">
                <Typography.Text copyable>{deviceName(editingAddress.device_id)}</Typography.Text>
              </Form.Item>
              <Form.Item name="network_id" hidden><InputNumber /></Form.Item>
              <Form.Item name="virtual_ip" label="虚拟 IP" rules={[{ required: true, message: '请输入虚拟 IP' }]}>
                <Input placeholder="172.16.10.x" />
              </Form.Item>
              <Form.Item name="hostname" label="主机名">
                <Input placeholder="office-pc" />
              </Form.Item>
              <Form.Item name="dns_enabled" label="Magic DNS 预留" valuePropName="checked">
                <Switch />
              </Form.Item>
              <Space>
                <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={addressMutation.isPending}>保存虚拟 IP</Button>
                <Button onClick={() => { setEditingAddress(null); addressForm.resetFields() }}>取消</Button>
              </Space>
            </Form>
          )}
          <Table
            rowKey={(row) => `${row.network_id}:${row.device_id}`}
            loading={addresses.isLoading}
            dataSource={addresses.data || []}
            columns={[
              { title: '设备', dataIndex: 'device_id', render: (v) => <Typography.Text copyable>{deviceName(v)}</Typography.Text> },
              { title: '虚拟 IP', dataIndex: 'virtual_ip', render: (v) => v ? <Typography.Text copyable>{v}</Typography.Text> : '-' },
              { title: '主机名', dataIndex: 'hostname', render: (v) => v || '-' },
              { title: 'Magic DNS', dataIndex: 'dns_enabled', render: (v) => <Tag color={v ? 'green' : 'default'}>{v ? '已预留' : '未启用'}</Tag> },
              {
                title: '设备密钥',
                render: (_, row) => {
                  const key = keyByDevice.get(row.device_id)
                  return key ? (
                    <Space direction="vertical" size={0}>
                      <Tag color="green">{key.algorithm || 'unknown'}</Tag>
                      <Typography.Text type="secondary" copyable>{key.public_key.slice(0, 16)}...</Typography.Text>
                    </Space>
                  ) : <Tag color="orange">缺失</Tag>
                },
              },
              { title: '更新时间', dataIndex: 'updated_at', render: formatTime },
              {
                title: '操作',
                width: 220,
                render: (_, row) => (
                  <Space>
                    <Tooltip title="编辑虚拟 IP">
                      <Button size="small" icon={<EditOutlined />} onClick={() => setEditingAddress(row)} />
                    </Tooltip>
                    <Tooltip title="触发重新 bootstrap">
                      <Button size="small" onClick={() => addressActionMutation.mutate({ action: 'bootstrap', row })} loading={addressActionMutation.isPending}>刷新</Button>
                    </Tooltip>
                    <Popconfirm title="重新分配虚拟 IP" description="将释放当前地址并分配该网络中的下一个可用地址。" onConfirm={() => addressActionMutation.mutate({ action: 'reassign', row })}>
                      <Button size="small" loading={addressActionMutation.isPending}>重分配</Button>
                    </Popconfirm>
                    <Popconfirm title="释放虚拟 IP" description="释放后，在线设备下一次 bootstrap 会自动重新分配。" onConfirm={() => addressActionMutation.mutate({ action: 'release', row })}>
                      <Button size="small" danger loading={addressActionMutation.isPending}>释放</Button>
                    </Popconfirm>
                  </Space>
                ),
              },
            ]}
            scroll={{ x: 900 }}
          />
        </Card>
      ),
    },
    {
      key: 'groups',
      label: '设备组',
      children: (
        <Card>
          <div className="lan-section-toolbar">
            <Typography.Text type="secondary">设备组用于 ACL 的源组和目标组匹配。</Typography.Text>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditingGroup(null)
                groupForm.setFieldsValue({ id: '', name: '', device_ids: [] })
                setGroupFormOpen(true)
              }}
            >
              新增设备组
            </Button>
          </div>
          {groupFormOpen && (
            <Form form={groupForm} layout="vertical" className="lan-form" onFinish={(values) => groupMutation.mutate(values)}>
              <Form.Item name="id" label="组 ID" rules={[{ required: true, message: '请输入组 ID' }]}>
                <Input disabled={Boolean(editingGroup)} placeholder="ops" />
              </Form.Item>
              <Form.Item name="name" label="组名称" rules={[{ required: true, message: '请输入组名称' }]}>
                <Input placeholder="运维组" />
              </Form.Item>
              <Form.Item name="device_ids" label="成员设备">
                <Select mode="multiple" allowClear options={deviceOptions} optionFilterProp="label" placeholder="选择成员设备" />
              </Form.Item>
              <Space>
                <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={groupMutation.isPending}>保存设备组</Button>
                <Button onClick={() => { setEditingGroup(null); setGroupFormOpen(false); groupForm.resetFields() }}>取消</Button>
              </Space>
            </Form>
          )}
          <Table
            rowKey="id"
            loading={groups.isLoading}
            dataSource={groups.data || []}
            columns={[
              { title: '组 ID', dataIndex: 'id', render: (v) => <Typography.Text copyable>{v}</Typography.Text> },
              { title: '组名称', dataIndex: 'name' },
              { title: '成员', dataIndex: 'device_ids', render: (ids: string[]) => ids?.length ? ids.map((id) => <Tag key={id}>{deviceName(id)}</Tag>) : '-' },
              { title: '更新时间', dataIndex: 'updated_at', render: formatTime },
              {
                title: '操作',
                width: 140,
                render: (_, row) => (
                  <Space>
                    <Tooltip title="编辑设备组">
                      <Button size="small" icon={<EditOutlined />} onClick={() => setEditingGroup(row)} />
                    </Tooltip>
                    <Popconfirm title="删除设备组" description="确认删除这个设备组？相关 ACL 需要另行调整。" onConfirm={() => deleteGroupMutation.mutate(row.id)}>
                      <Button size="small" danger icon={<DeleteOutlined />} loading={deleteGroupMutation.isPending} />
                    </Popconfirm>
                  </Space>
                ),
              },
            ]}
            scroll={{ x: 1000 }}
          />
        </Card>
      ),
    },
    {
      key: 'acl',
      label: 'ACL 规则',
      children: (
        <Card>
          <div className="lan-section-toolbar">
            <Typography.Text type="secondary">默认允许同组设备互通，可用 ACL 规则收紧访问范围。</Typography.Text>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditingACL(null)
                aclForm.setFieldsValue(defaultACLForm(networkID))
                setACLFormOpen(true)
              }}
              disabled={!networkID}
            >
              新增 ACL
            </Button>
          </div>
          {aclFormOpen && (
            <Form form={aclForm} layout="vertical" className="lan-acl-form" onFinish={(values) => aclMutation.mutate(normalizeACLForm(values))}>
              <Form.Item name="network_id" hidden><InputNumber /></Form.Item>
              <Form.Item name="source_device_id" label="源设备">
                <Select allowClear options={deviceOptions} placeholder="任意设备" />
              </Form.Item>
              <Form.Item name="source_group_id" label="源设备组">
                <Select allowClear options={groupOptions} placeholder="任意设备组" />
              </Form.Item>
              <Form.Item name="target_device_id" label="目标设备">
                <Select allowClear options={deviceOptions} placeholder="任意设备" />
              </Form.Item>
              <Form.Item name="target_group_id" label="目标设备组">
                <Select allowClear options={groupOptions} placeholder="任意设备组" />
              </Form.Item>
              <Form.Item name="protocol" label="协议" rules={[{ required: true, message: '请选择协议' }]}>
                <Select options={[
                  { label: '任意', value: 'any' },
                  { label: 'TCP', value: 'tcp' },
                  { label: 'UDP', value: 'udp' },
                  { label: 'ICMP', value: 'icmp' },
                ]} />
              </Form.Item>
              <Form.Item name="port_start" label="起始端口">
                <InputNumber min={0} max={65535} className="full-width" />
              </Form.Item>
              <Form.Item name="port_end" label="结束端口">
                <InputNumber min={0} max={65535} className="full-width" />
              </Form.Item>
              <Form.Item name="action" label="动作" rules={[{ required: true, message: '请选择动作' }]}>
                <Select options={[
                  { label: '允许', value: 'allow' },
                  { label: '拒绝', value: 'deny' },
                ]} />
              </Form.Item>
              <Form.Item name="enabled" label="启用" valuePropName="checked">
                <Switch />
              </Form.Item>
              <Space>
                <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={aclMutation.isPending}>保存 ACL</Button>
                <Button onClick={() => { setEditingACL(null); setACLFormOpen(false); aclForm.resetFields() }}>取消</Button>
              </Space>
            </Form>
          )}
          <Table
            rowKey="id"
            loading={acl.isLoading}
            dataSource={acl.data || []}
            columns={[
              { title: '状态', dataIndex: 'enabled', width: 90, render: (v) => <Tag color={v ? 'green' : 'default'}>{v ? '启用' : '停用'}</Tag> },
              { title: '源设备', dataIndex: 'source_device_id', render: (v) => deviceName(v) },
              { title: '源组', dataIndex: 'source_group_id', render: (v) => groupName(v) },
              { title: '目标设备', dataIndex: 'target_device_id', render: (v) => deviceName(v) },
              { title: '目标组', dataIndex: 'target_group_id', render: (v) => groupName(v) },
              { title: '协议', dataIndex: 'protocol', render: (v) => <Tag>{formatProtocol(v)}</Tag> },
              { title: '端口', render: (_, row) => formatPortRange(row.port_start, row.port_end) },
              { title: '动作', dataIndex: 'action', render: (v) => <Tag color={v === 'deny' ? 'red' : 'green'}>{v === 'deny' ? '拒绝' : '允许'}</Tag> },
              { title: '更新时间', dataIndex: 'updated_at', render: formatTime },
              {
                title: '操作',
                width: 140,
                render: (_, row) => (
                  <Space>
                    <Tooltip title="编辑 ACL">
                      <Button size="small" icon={<EditOutlined />} onClick={() => setEditingACL(row)} />
                    </Tooltip>
                    <Popconfirm title="删除 ACL" description="确认删除这条 ACL 规则？" onConfirm={() => deleteACLMutation.mutate(row.id)}>
                      <Tooltip title="删除 ACL">
                        <Button size="small" danger icon={<DeleteOutlined />} loading={deleteACLMutation.isPending} />
                      </Tooltip>
                    </Popconfirm>
                  </Space>
                ),
              },
            ]}
            scroll={{ x: 1100 }}
          />
        </Card>
      ),
    },
    {
      key: 'routes',
      label: '虚拟路由',
      children: (
        <Card>
          <div className="lan-section-toolbar">
            <Typography.Text type="secondary">第一阶段仅支持子网宣告，不支持默认路由、出口网关或 DNS 路由。</Typography.Text>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditingRoute(null)
                routeForm.setFieldsValue(defaultRouteForm(networkID))
                setRouteFormOpen(true)
              }}
              disabled={!networkID}
            >
              新增路由
            </Button>
          </div>
          {routeFormOpen && (
            <Form form={routeForm} layout="vertical" className="lan-form" onFinish={(values) => routeMutation.mutate(normalizeRouteForm(values))}>
              <Form.Item name="network_id" hidden><InputNumber /></Form.Item>
              <Form.Item name="device_id" label="宣告设备" rules={[{ required: true, message: '请选择宣告设备' }]}>
                <Select showSearch options={deviceOptions} optionFilterProp="label" placeholder="选择设备" />
              </Form.Item>
              <Form.Item
                name="cidr"
                label="子网 CIDR"
                rules={[
                  { required: true, message: '请输入子网 CIDR' },
                  { pattern: /^\d{1,3}(\.\d{1,3}){3}\/\d{1,2}$/, message: '请输入 CIDR，例如 192.168.1.0/24' },
                  {
                    validator: (_, value) => value === '0.0.0.0/0'
                      ? Promise.reject(new Error('第一阶段不支持默认路由'))
                      : Promise.resolve(),
                  },
                ]}
              >
                <Input placeholder="192.168.1.0/24" />
              </Form.Item>
              <Form.Item name="advertise" label="宣告给其他设备" valuePropName="checked">
                <Switch />
              </Form.Item>
              <Form.Item name="accept" label="接受该子网路由" valuePropName="checked">
                <Switch />
              </Form.Item>
              <Space>
                <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={routeMutation.isPending}>保存路由</Button>
                <Button onClick={() => { setEditingRoute(null); setRouteFormOpen(false); routeForm.resetFields() }}>取消</Button>
              </Space>
            </Form>
          )}
          <Table
            rowKey="id"
            loading={routes.isLoading}
            dataSource={routes.data || []}
            columns={[
              { title: '宣告设备', dataIndex: 'device_id', render: (v) => deviceName(v) },
              { title: '子网 CIDR', dataIndex: 'cidr', render: (v) => <Typography.Text copyable>{v}</Typography.Text> },
              { title: '宣告', dataIndex: 'advertise', render: (v) => <Tag color={v ? 'green' : 'default'}>{v ? '开启' : '关闭'}</Tag> },
              { title: '接受', dataIndex: 'accept', render: (v) => <Tag color={v ? 'blue' : 'default'}>{v ? '开启' : '关闭'}</Tag> },
              { title: '更新时间', dataIndex: 'updated_at', render: formatTime },
              {
                title: '操作',
                width: 140,
                render: (_, row) => (
                  <Space>
                    <Tooltip title="编辑路由">
                      <Button size="small" icon={<EditOutlined />} onClick={() => setEditingRoute(row)} />
                    </Tooltip>
                    <Popconfirm title="删除路由" description="确认删除这条虚拟路由？" onConfirm={() => deleteRouteMutation.mutate(row.id)}>
                      <Tooltip title="删除路由">
                        <Button size="small" danger icon={<DeleteOutlined />} loading={deleteRouteMutation.isPending} />
                      </Tooltip>
                    </Popconfirm>
                  </Space>
                ),
              },
            ]}
            scroll={{ x: 1000 }}
          />
        </Card>
      ),
    },
    {
      key: 'device-status',
      label: '设备状态',
      children: (
        <Card>
          <Table
            rowKey={(row) => `${row.network_id}:${row.device_id}`}
            loading={deviceStates.isLoading}
            dataSource={deviceStates.data || []}
            columns={[
              { title: '设备', dataIndex: 'device_id', render: (v) => deviceName(v) },
              { title: '虚拟 IP', dataIndex: 'virtual_ip', render: (v) => v ? <Typography.Text copyable>{v}</Typography.Text> : '-' },
              { title: '主机名', dataIndex: 'hostname', render: (v) => v || '-' },
              { title: '虚拟网卡', dataIndex: 'adapter_state', render: (v) => <Tag color={v === 'up' ? 'green' : v === 'error' ? 'red' : 'default'}>{v || '-'}</Tag> },
              { title: '选择网段', dataIndex: 'selected_cidr', render: (v) => v || '-' },
              { title: '路由冲突', dataIndex: 'route_conflict', render: (v) => v || '-' },
              { title: 'P2P/Relay/Down', render: (_, row) => `${row.p2p_peers || 0} / ${row.relay_peers || 0} / ${row.down_peers || 0}` },
              { title: '最近 Bootstrap', dataIndex: 'last_bootstrap_at', render: formatTime },
              { title: '最近状态', dataIndex: 'last_status_at', render: formatTime },
              { title: '最近错误', dataIndex: 'last_error', render: (v) => v || '-' },
            ]}
            scroll={{ x: 1400 }}
          />
        </Card>
      ),
    },
    {
      key: 'status',
      label: 'LAN 状态',
      children: (
        <Card>
          <Table
            rowKey={(row) => `${row.network_id}:${row.device_id}:${row.peer_id}`}
            loading={states.isLoading}
            dataSource={states.data || []}
            columns={[
              { title: '设备', dataIndex: 'device_id', render: (v) => deviceName(v) },
              { title: '对端', dataIndex: 'peer_id', render: (v) => deviceName(v) },
              { title: '虚拟网卡', dataIndex: 'adapter_state', render: (v) => <Tag color={v === 'up' ? 'green' : 'default'}>{v || '-'}</Tag> },
              { title: 'Peer Path', dataIndex: 'path', render: (v) => <Tag color={v === 'p2p' ? 'cyan' : v === 'relay' ? 'purple' : 'default'}>{v || '-'}</Tag> },
              { title: '数据路径', dataIndex: 'data_path', render: (v) => <Tag color={pathTagColor(v)}>{v || '-'}</Tag> },
              { title: '路径原因', dataIndex: 'path_reason', render: (v) => v || '-' },
              { title: '流量类别', dataIndex: 'traffic_class', render: (v) => v ? <Tag>{v}</Tag> : '-' },
              { title: 'RTT', dataIndex: 'rtt_ms', render: (v) => v ? `${v} ms` : '-' },
              { title: '估算速率', dataIndex: 'estimated_bps', render: (v) => v ? `${formatBytes(v)}/s` : '-' },
              { title: 'MTU/MSS', render: (_, row) => row.mtu ? `${row.mtu} / ${row.mss || '-'}` : '-' },
              { title: 'TX/RX', render: (_, row) => `${formatBytes(row.tx_bytes)} / ${formatBytes(row.rx_bytes)}` },
              { title: 'Drop Reason', dataIndex: 'drop_reason', render: (v) => v || '-' },
              { title: '路由冲突', dataIndex: 'route_conflict', render: (v) => v || '-' },
              { title: '握手时间', dataIndex: 'last_handshake_at', render: formatTime },
              { title: '最近错误', dataIndex: 'last_error', render: (v) => v || '-' },
              { title: '更新时间', dataIndex: 'updated_at', render: formatTime },
            ]}
            scroll={{ x: 1500 }}
          />
        </Card>
      ),
    },
    {
      key: 'path-events',
      label: '路径时间线',
      children: (
        <Card>
          <Table
            rowKey={(row) => row.id}
            loading={pathEvents.isLoading}
            dataSource={pathEvents.data || []}
            columns={[
              { title: '时间', dataIndex: 'created_at', render: formatTime },
              { title: '设备', dataIndex: 'device_id', render: (v) => deviceName(v) },
              { title: '对端', dataIndex: 'peer_id', render: (v) => deviceName(v) },
              { title: 'Peer Path', dataIndex: 'path', render: (v) => <Tag color={v === 'p2p' ? 'cyan' : v === 'relay' ? 'purple' : 'default'}>{v || '-'}</Tag> },
              { title: '数据路径', dataIndex: 'data_path', render: (v) => <Tag color={pathTagColor(v)}>{v || '-'}</Tag> },
              { title: '切换原因', dataIndex: 'path_reason', render: (v) => v || '-' },
              { title: '流量类别', dataIndex: 'traffic_class', render: (v) => v ? <Tag>{v}</Tag> : '-' },
              { title: 'TX/RX', render: (_, row) => `${formatBytes(row.tx_bytes)} / ${formatBytes(row.rx_bytes)}` },
            ]}
            scroll={{ x: 1200 }}
          />
        </Card>
      ),
    },
  ]

  return (
    <div className="page-stack">
      <div className="page-toolbar">
        <div>
          <Typography.Title level={3}>虚拟局域网</Typography.Title>
          <Typography.Text type="secondary">管理默认虚拟网络、设备虚拟 IP、ACL 和 LAN 运行状态。</Typography.Text>
        </div>
        <Space>
          <Button icon={<DownloadOutlined />} onClick={downloadDiagnostics} disabled={!networkID}>
            导出诊断
          </Button>
          <Button icon={<ReloadOutlined />} onClick={refreshLAN} loading={networks.isFetching || addresses.isFetching || acl.isFetching || states.isFetching || pathEvents.isFetching}>
            刷新
          </Button>
        </Space>
      </div>
      <Tabs items={tabs} />
    </div>
  )
}

function normalizeACLForm(rule: Partial<VirtualACLRulePayload>): ACLForm {
  return {
    network_id: Number(rule.network_id || 0),
    source_device_id: rule.source_device_id || '',
    source_group_id: rule.source_group_id || '',
    target_device_id: rule.target_device_id || '',
    target_group_id: rule.target_group_id || '',
    protocol: rule.protocol || 'tcp',
    port_start: Number(rule.port_start || 0),
    port_end: Number(rule.port_end || 0),
    action: rule.action || 'allow',
    enabled: rule.enabled !== false,
  }
}

function defaultACLForm(networkID?: number): ACLForm {
  return normalizeACLForm({ network_id: networkID || 0, protocol: 'tcp', action: 'allow', enabled: true })
}

function normalizeRouteForm(route: Partial<VirtualRoutePayload>): RouteForm {
  return {
    network_id: Number(route.network_id || 0),
    device_id: route.device_id || '',
    cidr: route.cidr || '',
    advertise: route.advertise !== false,
    accept: route.accept !== false,
  }
}

function defaultRouteForm(networkID?: number): RouteForm {
  return normalizeRouteForm({ network_id: networkID || 0, advertise: true, accept: true })
}

function formatProtocol(protocol?: string) {
  const value = protocol || 'tcp'
  return value === 'any' ? '任意' : value.toUpperCase()
}

function formatPortRange(start?: number, end?: number) {
  if (!start && !end) return '任意'
  if (start === end || !end) return start
  return `${start}-${end}`
}

function formatTime(value?: string) {
  return value ? dayjs(value).format('YYYY-MM-DD HH:mm:ss') : '-'
}

function formatBytes(value?: number) {
  const n = Number(value || 0)
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function pathTagColor(value?: string) {
  switch (value) {
    case 'p2p_datagram':
      return 'green'
    case 'p2p_kcp':
      return 'cyan'
    case 'relay_udp':
      return 'blue'
    case 'relay_http':
      return 'purple'
    default:
      return 'default'
  }
}
