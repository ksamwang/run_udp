import { DeleteOutlined, EditOutlined, ExperimentOutlined, PlusOutlined, ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Form, Input, InputNumber, message, Popconfirm, Select, Space, Switch, Table, Tabs, Tag, Tooltip, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { useEffect, useMemo, useState } from 'react'
import { listDevices } from '../api/devices'
import {
  createVirtualACLRule,
  deleteVirtualACLRule,
  listVirtualACLRules,
  listVirtualAddresses,
  listVirtualNetworks,
  listVirtualPeerStates,
  updateVirtualACLRule,
  updateVirtualAddress,
  updateVirtualNetwork,
} from '../api/lan'
import type { Device, VirtualACLRule, VirtualACLRulePayload, VirtualAddress, VirtualNetwork } from '../types/api'

type NetworkForm = Pick<VirtualNetwork, 'name' | 'cidr' | 'enabled'>
type AddressForm = Pick<VirtualAddress, 'network_id' | 'virtual_ip' | 'hostname' | 'dns_enabled'>
type ACLForm = VirtualACLRulePayload

export function LanPage() {
  const queryClient = useQueryClient()
  const [networkForm] = Form.useForm<NetworkForm>()
  const [addressForm] = Form.useForm<AddressForm>()
  const [aclForm] = Form.useForm<ACLForm>()
  const [editingAddress, setEditingAddress] = useState<VirtualAddress | null>(null)
  const [editingACL, setEditingACL] = useState<VirtualACLRule | null>(null)
  const [aclFormOpen, setACLFormOpen] = useState(false)

  const networks = useQuery({ queryKey: ['lan', 'networks'], queryFn: listVirtualNetworks })
  const defaultNetwork = networks.data?.[0]
  const networkID = defaultNetwork?.id
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
  const devices = useQuery({ queryKey: ['devices'], queryFn: listDevices })

  useEffect(() => {
    if (defaultNetwork) {
      networkForm.setFieldsValue({
        name: defaultNetwork.name,
        cidr: defaultNetwork.cidr,
        enabled: defaultNetwork.enabled,
      })
    }
  }, [defaultNetwork, networkForm])

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

  const deviceOptions = useMemo(() => (devices.data || []).map((device) => ({
    label: device.name ? `${device.name} (${device.id})` : device.id,
    value: device.id,
  })), [devices.data])
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

  const refreshLAN = () => {
    queryClient.invalidateQueries({ queryKey: ['lan'] })
  }
  const networkMutation = useMutation({
    mutationFn: (payload: NetworkForm) => updateVirtualNetwork(defaultNetwork!.id, payload),
    onSuccess: () => {
      message.success('虚拟网络已保存')
      refreshLAN()
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '保存失败'),
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
            description="第一版只开放默认网络，数据模型已支持后续多个虚拟网络。"
          />
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
            <Form.Item name="enabled" label="启用 LAN" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={networkMutation.isPending} disabled={!defaultNetwork}>
              保存网络配置
            </Button>
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
              { title: '更新时间', dataIndex: 'updated_at', render: formatTime },
              {
                title: '操作',
                width: 90,
                render: (_, row) => (
                  <Tooltip title="编辑虚拟 IP">
                    <Button size="small" icon={<EditOutlined />} onClick={() => setEditingAddress(row)} />
                  </Tooltip>
                ),
              },
            ]}
            scroll={{ x: 900 }}
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
              <Form.Item name="target_device_id" label="目标设备">
                <Select allowClear options={deviceOptions} placeholder="任意设备" />
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
              { title: '目标设备', dataIndex: 'target_device_id', render: (v) => deviceName(v) },
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
              { title: 'RTT', dataIndex: 'rtt_ms', render: (v) => v ? `${v} ms` : '-' },
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
  ]

  return (
    <div className="page-stack">
      <div className="page-toolbar">
        <div>
          <Typography.Title level={3}>虚拟局域网</Typography.Title>
          <Typography.Text type="secondary">管理默认虚拟网络、设备虚拟 IP、ACL 和 LAN 运行状态。</Typography.Text>
        </div>
        <Button icon={<ReloadOutlined />} onClick={refreshLAN} loading={networks.isFetching || addresses.isFetching || acl.isFetching || states.isFetching}>
          刷新
        </Button>
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
