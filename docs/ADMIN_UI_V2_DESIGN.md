# OmniProxy 轻量后台 V2 设计

**日期**：2026-05-15
**状态**：方案设计

---

## 1. 设计原则

- **轻量**：纯 HTML+CSS+JS 内嵌，不引入前端框架，单二进制部署不变
- **双视角**：管理员看全部，用户只看自己的
- **认证统一**：用现有的 API Key 做身份认证，不另搞账号体系

---

## 2. 角色定义

| 角色 | 认证方式 | 能看到什么 |
|------|----------|-----------|
| **管理员** | Admin Key (`admin_key`) | 所有 key、所有用量、Provider 状态、Codex 账号 |
| **用户** | 普通 API Key | 自己的用量、可用模型、key 信息 |

判断逻辑：key == config.admin_key → 管理员，否则在 config.keys 里找到匹配 → 用户。

---

## 3. 页面结构

```
/admin/          → 后台入口（自动识别角色）
/admin/ui        → 管理员视图
/admin/user      → 用户视图
```

### 3.1 登录页

```
┌─────────────────────────────┐
│     🤖 OmniProxy            │
│                             │
│  ┌─────────────────────┐    │
│  │ 输入你的 API Key     │    │
│  └─────────────────────┘    │
│                             │
│       [ 登 录 ]             │
│                             │
│  Key 仅存本地浏览器         │
└─────────────────────────────┘
```

- 输入 API Key，点登录
- 后端判断是 Admin Key 还是普通 Key
- 返回角色信息，前端跳转到对应视图

### 3.2 管理员视图

顶部导航标签页：

#### 📊 统计（已有，优化）
- 总览卡片：总请求数、Prompt/Completion/Total Tokens
- 按 Key 统计表格
- 按模型统计表格
- 时间筛选：7天 / 30天 / 90天

#### 🔑 Key 管理（新增）
- Key 列表表格：

| 别名 | Key（脱敏） | Provider | 模型限制 | 状态 | 操作 |
|------|------------|----------|----------|------|------|
| 用户1 | sk-qmbit-****-06T | 不限制 | 全部 | ✅ 活跃 | 编辑 删除 |
| 用户2 | sk-qmbit-****-znW | 不限制 | 全部 | ✅ 活跃 | 编辑 删除 |
| Hermes | sk-qmbit-****-5da | 不限制 | 全部 | ✅ 活跃 | 编辑 删除 |

- **新增 Key**：弹出框，填写别名、选择 provider 限制、模型限制
- **编辑 Key**：修改别名、provider、模型限制
- **删除 Key**：确认后删除
- Key 脱敏规则：保留前8位 + 后4位，中间用 `****`

> 注意：Key 的增删改只影响运行时内存，需要同步写入 config.yaml 持久化

#### 🔌 Provider 状态（已有）
- Codex / MiniMax / 智谱 的状态卡片
- 额度使用进度条

#### ⚙️ 系统设置（新增）
- 当前配置概览（端口、Provider 列表）
- 运行时长
- 版本信息

### 3.3 用户视图

简化版，只看自己的数据：

#### 📊 我的用量
- 我的请求数、Token 用量（卡片）
- 最近 N 天用量趋势
- 按模型分组统计

#### 🔑 我的 Key
- Key 信息（脱敏显示）
- 绑定的 Provider
- 可用模型列表

#### 📋 可用模型
- 模型列表（从 /v1/models 获取）
- 每个 model 的 provider 标注

---

## 4. API 设计

### 4.1 登录认证

```
GET /admin/auth?check
Authorization: Bearer <key>

Response:
{
  "role": "admin" | "user",
  "alias": "Admin",
  "key_hash": "xxxx"
}
```

### 4.2 管理员 API

```
GET /admin/keys              # 列出所有 key（脱敏）
POST /admin/keys             # 新增 key
PUT /admin/keys/:alias       # 修改 key
DELETE /admin/keys/:alias    # 删除 key
GET /admin/stats             # 统计（已有）
GET /admin/usage             # Provider 用量（已有）
GET /admin/config            # 配置概览（不含敏感 key）
```

### 4.3 用户 API

```
GET /admin/user/stats        # 我的用量统计
GET /admin/user/info         # 我的 key 信息
GET /admin/user/models       # 我可用的模型
```

---

## 5. Key 管理持久化

Key 的增删改需要持久化到 config.yaml：

```
修改 Key 流程：
1. 更新内存中的 config.Keys
2. 重写 config.yaml（保留注释和格式）
3. 重新构建索引（keyIndex、modelProviderIndex）
```

为安全起见：
- 新增/删除 Key **需要确认弹窗**
- Admin Key **不允许通过 UI 删除**
- 修改操作**记录日志**

---

## 6. 文件改动

| 文件 | 改动 |
|------|------|
| `admin/admin.go` | 重构：新增 Key 管理 handler、用户视角 handler |
| `config/config.go` | 新增 Key CRUD 方法、配置文件回写 |
| `main.go` | 新增路由注册 |
| `db/db.go` | 新增按 key_alias 查询用量的方法 |

---

## 7. 实现计划

### Phase 1：Key 管理（管理员）
**目标**：能看、能增删改 key

| 任务 | 预估 |
|------|------|
| `/admin/keys` 列表 API | 1h |
| Key CRUD + config.yaml 回写 | 2h |
| Key 管理页面 HTML/JS | 2h |
| 登录页 + 角色判断 | 1h |
| **合计** | **6h** |

### Phase 2：用户视角
**目标**：用户登录看自己的用量

| 任务 | 预估 |
|------|------|
| `/admin/user/*` API | 1.5h |
| 用户视图 HTML/JS | 1.5h |
| **合计** | **3h** |

### Phase 3：打磨
**目标**：体验优化

| 任务 | 预估 |
|------|------|
| 用量趋势图（轻量 Canvas） | 2h |
| 系统设置页 | 0.5h |
| 响应式适配 | 0.5h |
| **合计** | **3h** |

---

## 8. 安全考虑

- Key 脱敏：只显示前8位 + 后4位
- Admin Key 只能查看，不能删除/修改
- 所有写操作需要确认
- Key 仅存在浏览器 localStorage
- 不暴露任何 Provider 的真实 API Key
