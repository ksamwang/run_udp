import { Tag } from 'antd'

type StatusTagProps = {
  state?: string
  enabled?: boolean
  online?: boolean
}

export function StatusTag({ state, enabled, online }: StatusTagProps) {
  if (typeof enabled === 'boolean' && !enabled) {
    return <Tag>已禁用</Tag>
  }
  if (typeof online === 'boolean') {
    return online ? <Tag color="green">在线</Tag> : <Tag color="red">离线</Tag>
  }
  switch ((state || '').toLowerCase()) {
    case 'p2p':
      return <Tag color="green">P2P</Tag>
    case 'relay':
      return <Tag color="blue">Relay</Tag>
    case 'connecting':
      return <Tag color="gold">连接中</Tag>
    case 'backoff':
      return <Tag color="orange">重试中</Tag>
    case 'disabled':
      return <Tag>已禁用</Tag>
    case 'down':
      return <Tag color="red">离线</Tag>
    default:
      return <Tag>未知</Tag>
  }
}
