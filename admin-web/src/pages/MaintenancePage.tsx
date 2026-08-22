import { CheckCircleOutlined, CloudDownloadOutlined, DeleteOutlined, SafetyCertificateOutlined } from "@ant-design/icons";
import { Alert, Button, Card, Col, Descriptions, Form, InputNumber, Modal, Row, Space, Table, Tag, Typography, message } from "antd";
import { useCallback, useEffect, useState } from "react";
import type { AdminAPI } from "../api";
import { PageTitle } from "../components/PageTitle";
import { formatBytes, formatTime, pathName, shortID } from "../format";
import type { BackupRun, DoctorReport, GCPlan } from "../types";

export function MaintenancePage({ api }: { api: AdminAPI }) {
  const [doctor, setDoctor] = useState<DoctorReport>();
  const [backups, setBackups] = useState<BackupRun[]>([]);
  const [plan, setPlan] = useState<GCPlan>();
  const [loading, setLoading] = useState(true);
  const [messageAPI, contextHolder] = message.useMessage();
  const [form] = Form.useForm<{ retention: number; versions: number }>();
  const load = useCallback(async () => {
    setLoading(true);
    try { const [report, runs] = await Promise.all([api.doctor(), api.backups()]); setDoctor(report); setBackups(runs); }
    catch (reason) { void messageAPI.error(reason instanceof Error ? reason.message : String(reason)); }
    finally { setLoading(false); }
  }, [api, messageAPI]);
  useEffect(() => { void load(); }, [load]);

  const createBackup = async () => {
    const result = await api.createBackup();
    void messageAPI.success(`备份已创建：${pathName(result.destination)}`);
    await load();
  };
  const verify = async (id: string) => {
    await api.verifyBackup(id);
    void messageAPI.success("备份文件集、哈希与 SQLite 完整性均通过");
  };
  const createPlan = async () => {
    const values = await form.validateFields();
    setPlan(await api.planGC(values.retention, values.versions));
  };
  const executePlan = () => {
    if (!plan) return;
    Modal.confirm({
      title: "执行这份垃圾回收计划？", width: 560,
      content: <><Alert type="warning" showIcon message="此操作会永久删除计划中列出的历史数据" /><Typography.Paragraph className="confirm-hash">计划哈希：<Typography.Text code copyable>{plan.hash}</Typography.Text></Typography.Paragraph></>,
      okText: "确认执行", okButtonProps: { danger: true }, cancelText: "取消",
      onOk: async () => { const result = await api.executeGC(plan.id, plan.hash); setPlan(undefined); await load(); void messageAPI.success(`已回收 ${formatBytes(result.bytes_reclaimed)}`); }
    });
  };

  return <>
    {contextHolder}
    <PageTitle title="维护与备份" description="执行完整性检查、安全垃圾回收，以及受控目录内的在线备份。" onRefresh={() => void load()} refreshing={loading} />
    <Row gutter={[16, 16]}>
      <Col xs={24} xl={12}><Card title="数据检查" extra={<SafetyCertificateOutlined />}>
        <Alert type={doctor?.ok ? "success" : "warning"} showIcon message={doctor?.ok ? "检查通过" : "发现异常"} description={`SQLite: ${doctor?.integrity ?? "检查中"}；缺失块 ${doctor?.missing_chunk_hashes.length ?? 0}；损坏块 ${doctor?.corrupt_chunk_hashes.length ?? 0}；孤立块 ${doctor?.orphan_chunk_files.length ?? 0}`} />
      </Card></Col>
      <Col xs={24} xl={12}><Card title="垃圾回收计划" extra={<DeleteOutlined />}>
        <Form form={form} layout="inline" initialValues={{ retention: 90, versions: 20 }}>
          <Form.Item name="retention" label="保留天数" rules={[{ required: true }]}><InputNumber min={1} /></Form.Item>
          <Form.Item name="versions" label="版本数" rules={[{ required: true }]}><InputNumber min={1} /></Form.Item>
          <Form.Item><Button onClick={() => void createPlan()}>生成预览</Button></Form.Item>
        </Form>
      </Card></Col>
    </Row>
    {plan && <Card className="section-card" title="待执行的回收计划" extra={<Button danger type="primary" onClick={executePlan}>执行计划</Button>}>
      <Descriptions column={{ xs: 1, sm: 2, lg: 4 }} items={[
        { key: "id", label: "计划", children: shortID(plan.id) }, { key: "bytes", label: "预计回收", children: formatBytes(plan.estimated_bytes) },
        { key: "changes", label: "修订", children: plan.change_revisions.length }, { key: "paths", label: "已删路径", children: plan.deleted_paths.length },
        { key: "blobs", label: "Blob", children: plan.blob_hashes.length }, { key: "chunks", label: "分块", children: plan.chunk_hashes.length }
      ]} />
    </Card>}
    <Card className="section-card" title="托管备份" extra={<Button type="primary" icon={<CloudDownloadOutlined />} loading={loading} onClick={() => void createBackup()}>立即备份</Button>}>
      <Alert type="info" showIcon message="备份写入宿主机映射的 runtime-backups 目录" description="在线创建与校验可在此完成；灾难恢复需要停服后替换数据目录，避免运行中的数据库被覆盖。" />
      <Table className="nested-table" rowKey="id" dataSource={backups} pagination={false} columns={[
        { title: "备份", dataIndex: "destination", render: (value: string) => <><Typography.Text strong>{pathName(value)}</Typography.Text><br/><Typography.Text type="secondary" ellipsis={{ tooltip: value }}>{value}</Typography.Text></> },
        { title: "状态", dataIndex: "status", render: (value: string) => <Tag color={value === "completed" ? "green" : value === "failed" ? "red" : "blue"}>{value}</Tag> },
        { title: "完成时间", dataIndex: "completed_at", render: formatTime },
        { title: "操作", render: (_: unknown, row: BackupRun) => <Button size="small" icon={<CheckCircleOutlined />} disabled={row.status !== "completed"} onClick={() => void verify(row.id)}>校验</Button> }
      ]} />
    </Card>
  </>;
}
