import { CopyOutlined, KeyOutlined, LaptopOutlined, PlusOutlined } from "@ant-design/icons";
import { Alert, Button, Descriptions, Drawer, Flex, Form, Input, InputNumber, Modal, Select, Space, Table, Tag, Typography, message } from "antd";
import { useCallback, useEffect, useState } from "react";
import type { AdminAPI } from "../api";
import { PageTitle } from "../components/PageTitle";
import { formatBytes, formatTime, shortID } from "../format";
import type { Device, Vault } from "../types";

const defaultScopes = "sync:read,sync:write,history:read,restore:write";
type VaultForm = { id: string; display_name: string; quota_bytes: number; max_files: number; status: Vault["status"] };

export function VaultsPage({ api }: { api: AdminAPI }) {
  const [vaults, setVaults] = useState<Vault[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Vault | null | undefined>();
  const [devicesFor, setDevicesFor] = useState<Vault>();
  const [devices, setDevices] = useState<Device[]>([]);
  const [form] = Form.useForm<VaultForm>();
  const [messageAPI, contextHolder] = message.useMessage();

  const load = useCallback(async () => {
    setLoading(true); setError("");
    try { setVaults(await api.vaults()); }
    catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)); }
    finally { setLoading(false); }
  }, [api]);
  useEffect(() => { void load(); }, [load]);

  const openCreate = () => {
    form.setFieldsValue({ id: "", display_name: "", quota_bytes: 0, max_files: 0, status: "active" });
    setEditing(null);
  };
  const openEdit = (vault: Vault) => {
    form.setFieldsValue(vault);
    setEditing(vault);
  };
  const save = async () => {
    const values = await form.validateFields();
    if (editing) await api.updateVault(editing.id, values);
    else await api.createVault(values);
    setEditing(undefined);
    await load();
    void messageAPI.success(editing ? "Vault 已更新" : "Vault 已创建");
  };
  const pair = async (vault: Vault) => {
    const result = await api.createPairingCode(vault.id, 600, defaultScopes);
    Modal.info({
      title: `${vault.display_name} 的配对码`, width: 560,
      content: <Space direction="vertical" className="full-width">
        <Alert type="warning" showIcon message="配对码仅显示这一次，有效期 10 分钟" />
        <Input.TextArea value={result.code} readOnly autoSize={{ minRows: 2 }} />
        <Button icon={<CopyOutlined />} onClick={() => void navigator.clipboard.writeText(result.code).then(() => messageAPI.success("已复制"))}>复制配对码</Button>
        <Typography.Text type="secondary">失效时间：{formatTime(result.expires_at)}</Typography.Text>
      </Space>
    });
  };
  const showDevices = async (vault: Vault) => {
    setDevicesFor(vault);
    try { setDevices(await api.devices(vault.id)); }
    catch (reason) { void messageAPI.error(reason instanceof Error ? reason.message : String(reason)); }
  };
  const changeDevice = (device: Device, status: "retired" | "revoked") => {
    Modal.confirm({
      title: status === "revoked" ? "撤销设备访问？" : "将设备标记为退役？",
      content: status === "revoked" ? "撤销后该设备的 Token 会立即失效。" : "退役设备不再参与安全水位计算。",
      okText: status === "revoked" ? "确认撤销" : "确认退役", okButtonProps: { danger: status === "revoked" }, cancelText: "取消",
      onOk: async () => { await api.setDeviceStatus(device.vault_id, device.id, status); await showDevices(devicesFor!); }
    });
  };

  return <>
    {contextHolder}
    <PageTitle title="Vault 与设备" description="创建同步空间、生成一次性配对码并管理已配对设备。" onRefresh={() => void load()} refreshing={loading} action={<Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新建 Vault</Button>} />
    {error && <Alert type="error" showIcon message="读取 Vault 失败" description={error} />}
    <Table rowKey="id" loading={loading} dataSource={vaults} pagination={false} scroll={{ x: 900 }} columns={[
      { title: "名称", dataIndex: "display_name", render: (value: string, row: Vault) => <><Typography.Text strong>{value}</Typography.Text><br/><Typography.Text type="secondary" copyable>{row.id}</Typography.Text></> },
      { title: "状态", dataIndex: "status", render: (value: Vault["status"]) => <Tag color={value === "active" ? "green" : "orange"}>{value === "active" ? "正常" : "已暂停"}</Tag> },
      { title: "配额", render: (_: unknown, row: Vault) => row.quota_bytes ? formatBytes(row.quota_bytes) : "不限" },
      { title: "文件上限", dataIndex: "max_files", render: (value: number) => value || "不限" },
      { title: "操作", fixed: "right" as const, render: (_: unknown, row: Vault) => <Flex gap={6} wrap><Button size="small" onClick={() => openEdit(row)}>设置</Button><Button size="small" icon={<KeyOutlined />} onClick={() => void pair(row)}>配对</Button><Button size="small" icon={<LaptopOutlined />} onClick={() => void showDevices(row)}>设备</Button></Flex> }
    ]} />
    <Modal open={editing !== undefined} title={editing ? "编辑 Vault" : "新建 Vault"} okText="保存" cancelText="取消" onCancel={() => setEditing(undefined)} onOk={() => void save()} destroyOnHidden>
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item name="id" label="Vault ID" rules={[{ required: true }, { pattern: /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/u, message: "仅允许字母、数字、点、下划线和连字符" }]}><Input disabled={Boolean(editing)} placeholder="例如 notes" /></Form.Item>
        <Form.Item name="display_name" label="显示名称" rules={[{ required: true, max: 200 }]}><Input /></Form.Item>
        {editing && <Form.Item name="status" label="状态"><Select options={[{ value: "active", label: "正常" }, { value: "suspended", label: "暂停同步" }]} /></Form.Item>}
        <Form.Item name="quota_bytes" label="容量配额（字节，0 表示不限）" rules={[{ required: true }]}><InputNumber min={0} precision={0} className="full-width" /></Form.Item>
        <Form.Item name="max_files" label="文件数量上限（0 表示不限）" rules={[{ required: true }]}><InputNumber min={0} precision={0} className="full-width" /></Form.Item>
      </Form>
    </Modal>
    <Drawer open={Boolean(devicesFor)} title={`${devicesFor?.display_name ?? ""} 的设备`} width={720} onClose={() => setDevicesFor(undefined)}>
      <Table rowKey="id" dataSource={devices} pagination={false} expandable={{ expandedRowRender: device => <Descriptions size="small" column={1} items={[{ key: "id", label: "设备 ID", children: device.id }, { key: "version", label: "客户端", children: `${device.platform} / ${device.client_version}` }, { key: "seen", label: "最后活动", children: formatTime(device.last_seen_at) }, { key: "revision", label: "确认修订", children: device.last_ack_revision }]} /> }} columns={[
        { title: "设备", render: (_: unknown, row: Device) => <><Typography.Text strong>{row.name}</Typography.Text><br/><Typography.Text type="secondary">{shortID(row.id)}</Typography.Text></> },
        { title: "状态", dataIndex: "status", render: (value: string) => <Tag color={value === "active" ? "green" : "default"}>{value}</Tag> },
        { title: "操作", render: (_: unknown, row: Device) => row.status === "active" ? <Space><Button size="small" onClick={() => changeDevice(row, "retired")}>退役</Button><Button danger size="small" onClick={() => changeDevice(row, "revoked")}>撤销</Button></Space> : "—" }
      ]} />
    </Drawer>
  </>;
}
