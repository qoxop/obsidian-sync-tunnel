import { LockOutlined, SafetyCertificateOutlined } from "@ant-design/icons";
import { Alert, Button, Card, Form, Input, Space, Typography } from "antd";

interface LoginPageProps {
  loading: boolean;
  error: string;
  onLogin: (token: string) => Promise<void>;
}

export function LoginPage({ loading, error, onLogin }: LoginPageProps) {
  const [form] = Form.useForm<{ token: string }>();
  return (
    <main className="login-shell">
      <Card className="login-card" variant="borderless">
        <Space direction="vertical" size={20} className="full-width">
          <div className="brand-lockup">
            <div className="brand-icon"><SafetyCertificateOutlined /></div>
            <div>
              <Typography.Title level={2}>Sync Tunnel</Typography.Title>
              <Typography.Text type="secondary">本机管理中心</Typography.Text>
            </div>
          </div>
          <Alert
            type="info"
            showIcon
            message="页面仅由本机管理端口提供"
            description="Admin Token 只保存在当前浏览器标签页的 sessionStorage 中，不会写入服务器或前端构建产物。"
          />
          {error && <Alert type="error" showIcon message="登录失败" description={error} />}
          <Form
            form={form}
            layout="vertical"
            requiredMark={false}
            onFinish={({ token }) => void onLogin(token.trim())}
          >
            <Form.Item
              name="token"
              label="Admin Token"
              rules={[{ required: true, min: 32, message: "请输入 secrets/admin-token.txt 中的完整 Token" }]}
            >
              <Input.Password
                autoFocus
                autoComplete="current-password"
                prefix={<LockOutlined />}
                placeholder="粘贴本机 Admin Token"
                size="large"
              />
            </Form.Item>
            <Button type="primary" htmlType="submit" size="large" block loading={loading}>
              进入管理中心
            </Button>
          </Form>
          <Typography.Paragraph type="secondary" className="login-note">
            默认地址为 <Typography.Text code>http://127.0.0.1:8788/admin/</Typography.Text>。
            不要通过 Cloudflare 或局域网转发此端口。
          </Typography.Paragraph>
        </Space>
      </Card>
    </main>
  );
}
