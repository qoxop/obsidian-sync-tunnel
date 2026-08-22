import { CheckCircleOutlined, CloudServerOutlined, InfoCircleOutlined, WarningOutlined } from "@ant-design/icons";
import { Alert, Button, Card, Col, Form, Input, Row, Space, Table, Tag, Typography } from "antd";
import { useState } from "react";
import type { AdminAPI } from "../api";
import { PageTitle } from "../components/PageTitle";
import { formatTime } from "../format";
import type { ConnectivityCheck, ConnectivityReport } from "../types";

const publicURLKey = "sync-tunnel-public-url";

interface ConnectivityForm {
  publicURL: string;
  accessClientID?: string;
  accessClientSecret?: string;
}

const statusView = {
  pass: { color: "green", label: "正常", icon: <CheckCircleOutlined /> },
  warning: { color: "gold", label: "需确认", icon: <WarningOutlined /> },
  fail: { color: "red", label: "异常", icon: <WarningOutlined /> },
  info: { color: "blue", label: "信息", icon: <InfoCircleOutlined /> }
} as const;

export function ConnectivityPage({ api }: { api: AdminAPI }) {
  const [form] = Form.useForm<ConnectivityForm>();
  const [report, setReport] = useState<ConnectivityReport>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const runCheck = async () => {
    const values = await form.validateFields();
    setLoading(true);
    setError("");
    try {
      const publicURL = values.publicURL.trim();
      localStorage.setItem(publicURLKey, publicURL);
      setReport(await api.checkConnectivity({
        public_url: publicURL,
        access_client_id: values.accessClientID?.trim() || undefined,
        access_client_secret: values.accessClientSecret?.trim() || undefined
      }));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      setLoading(false);
    }
  };

  const overallType = report?.overall === "healthy" ? "success" : report?.overall === "warning" ? "warning" : "error";

  return <>
    <PageTitle title="连接诊断" description="一次检查本地服务、Cloudflare Tunnel、DNS 和公网同步入口。" />
    <Row gutter={[16, 16]}>
      <Col xs={24} xl={10}>
        <Card title="检查设置" extra={<CloudServerOutlined />}>
          <Form form={form} layout="vertical" initialValues={{ publicURL: localStorage.getItem(publicURLKey) ?? "" }}>
            <Form.Item name="publicURL" label="公网同步地址" extra="填写 Obsidian 插件使用的 Server URL。" rules={[
              { required: true, message: "请输入公网同步地址" },
              { type: "url", message: "请输入完整的 HTTPS URL" },
              { pattern: /^https:\/\//iu, message: "必须使用 HTTPS" }
            ]}>
              <Input placeholder="https://sync.example.com" autoComplete="url" />
            </Form.Item>
            <Typography.Title level={5}>Cloudflare Access（可选）</Typography.Title>
            <Typography.Paragraph type="secondary">仅用于本次检查，不会保存到服务器或浏览器。</Typography.Paragraph>
            <Form.Item name="accessClientID" label="Service Token Client ID">
              <Input autoComplete="off" />
            </Form.Item>
            <Form.Item name="accessClientSecret" label="Service Token Client Secret">
              <Input.Password autoComplete="new-password" />
            </Form.Item>
            <Button type="primary" block loading={loading} onClick={() => void runCheck()}>开始检查</Button>
          </Form>
        </Card>
      </Col>
      <Col xs={24} xl={14}>
        <Card title="检查结果">
          {!report && !error && <Alert type="info" showIcon message="尚未运行连接诊断" description="填写同步地址后点击“开始检查”。检查不会修改 Cloudflare 或 Clash Verge 配置。" />}
          {error && <Alert type="error" showIcon message="连接诊断请求失败" description={error} />}
          {report && <Space direction="vertical" size="middle" className="full-width">
            <Alert type={overallType} showIcon message={report.summary} description={`检查时间：${formatTime(report.checked_at)}`} />
            <Table<ConnectivityCheck> rowKey="id" dataSource={report.checks} pagination={false} size="small" columns={[
              { title: "检查项", dataIndex: "label", width: 150, render: (value: string) => <Typography.Text strong>{value}</Typography.Text> },
              { title: "状态", dataIndex: "status", width: 92, render: (value: ConnectivityCheck["status"]) => {
                const view = statusView[value];
                return <Tag color={view.color} icon={view.icon}>{view.label}</Tag>;
              } },
              { title: "结果与建议", render: (_: unknown, row: ConnectivityCheck) => <>
                <Typography.Text>{row.detail}</Typography.Text>
                {row.suggestion && <Typography.Paragraph type="secondary" className="diagnostic-suggestion">{row.suggestion}</Typography.Paragraph>}
              </> }
            ]} />
          </Space>}
        </Card>
      </Col>
    </Row>
    <Alert className="section-card" type="info" showIcon message="关于 Clash Verge TUN" description="检测到 198.18.0.0/15 或 fdfe:dcba:9876::/64 时，页面会提示应用 argotunnel.com Fake-IP 排除和 cloudflared.exe 直连规则。" />
  </>;
}
