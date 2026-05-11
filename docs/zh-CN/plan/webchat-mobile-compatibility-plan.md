# WebChat 移动端兼容详细规划

本文档用于基于当前 EvoDuck WebChat 现状，制定一份可执行的移动端兼容规划。

最终结论：

1. **不单独做一套移动端站点。**
2. **直接复用现有 Web，做响应式与交互折叠改造。**
3. **先改全局骨架，再优先把 Chat 做到手机可用，最后补齐管理页。**

---

## 1. 当前项目现状结论

### 1.1 前端技术形态

当前 Web 前端不是 React/Vue 这类组件化 SPA，而是：

1. 单个 `web/index.html`
2. 多个原生 JS 页面脚本
3. 多个按页面拆分的 CSS 文件
4. 全局共享状态主要放在 `web/js/app.js`

相关文件：

- `web/index.html:1`
- `web/js/app.js:1`
- `web/js/router.js:1`
- `web/css/style.css:1`
- `web/css/common.css:1`

这意味着移动端改造的关键不是“重写页面组件”，而是：

1. 重新组织全局布局层
2. 给现有 DOM 增加移动状态 class
3. 按页面补断点和交互折叠

### 1.2 当前页面组织方式

所有页面都在同一个 HTML 里，通过切换 `.page-container.active` 显示：

- `web/index.html:125`
- `web/js/router.js:3`

这点对移动端是好事，因为：

1. 不需要重做路由
2. 不需要单独维护移动版 URL
3. 页面切换后可以直接复用现有数据获取逻辑

### 1.3 当前全局布局是桌面优先固定三栏

目前主界面为：

1. 左侧 sidebar 固定 `260px`
2. 中间 main-content 固定占剩余宽度
3. 右侧 right-panel 固定 `232px`

相关位置：

- `web/css/common.css:152`
- `web/css/common.css:351`
- `web/css/common.css:666`

关键问题：

1. `body` 使用 `height: 100vh`、`width: 100vw`、`overflow: hidden`，见 `web/css/common.css:43`
2. 三栏都用了 `position: fixed`
3. 中间内容宽度直接由 `calc(100vw - 260px - 232px)` 计算，见 `web/css/common.css:352`
4. 当前没有全局移动端断点来重排三栏

这就是目前手机端不兼容的根本原因。

### 1.4 当前 JS 里没有移动端状态控制

我检查过 `web/js`，目前没有看到：

1. `matchMedia`
2. `resize` 断点状态
3. `visualViewport`
4. sidebar drawer 控制
5. right panel drawer 控制

这本身不一定是问题。结合当前项目结构，更合适的方向是：

1. **优先用 CSS media query 完成布局适配**
2. **避免用 JS 做断点判断、页面跳转或双布局切换**
3. **只有在 CSS 无法表达“临时开关状态”时，再补极少量 JS**

也就是说，移动端改造的主轴应当是响应式，而不是 JS 驱动布局分支。

### 1.5 当前已有局部响应式，但不成体系

已经有部分页面自己做了窄屏处理：

- Chat：`web/css/chat.css:590`
- Memory：`web/css/memory.css:166`
- Knowledge：`web/css/knowledge.css:503`
- Skills：`web/css/skills.css:226`
- Schedules：`web/css/schedules.css:230`
- Diagnostics：`web/css/diagnostics.css:299`

但这些都只是页面内部单列化，并没有解决：

1. 外层三栏骨架
2. 顶部 header 操作区拥挤
3. 导航入口缺失
4. 右侧面板在手机上无处安放

所以当前状态更准确地说是：**局部有响应式，整体没有移动方案。**

---

## 2. 为什么现在不建议单独做一套移动端

### 2.1 代码复用价值很高

当前以下逻辑已经全部打通：

1. WebSocket 连接
2. 页面切换
3. 消息流处理
4. Agent/Sessions/Skills/Settings/Diagnostics 数据请求
5. 图片上传与聊天输入

这些逻辑主要集中在：

- `web/js/app.js:41`
- `web/js/chat.js:669`
- `web/js/chat-page.js:3`
- 其他 `*-page.js`

如果拆一套移动端：

1. UI 可以复写
2. 但状态、连接、消息、能力检测、上传等逻辑仍要再接一次
3. 对当前原生 JS 结构来说，重复成本会很高

### 2.2 当前需求更像“兼容手机”，不是“做移动产品”

从产品形态看，现有 WebChat 本质还是一个带管理能力的控制台，不是纯聊天 IM。

如果现在直接做两套前端，会很快遇到这些问题：

1. 哪些页面只在桌面保留
2. 哪些页面移动端也做
3. 数据结构和按钮状态怎么同步
4. 后续新功能要改一套还是两套

所以当前阶段最合理的策略仍然是：

**一套应用，两套布局。**

---

## 3. 推荐目标定义

本次移动端兼容建议分成两个层级目标。

### 3.1 第一层目标：可用

手机上至少做到：

1. 能打开
2. 能连接
3. 能聊天
4. 能查看主要页面
5. 不出现明显横向溢出和操作阻塞

### 3.2 第二层目标：顺手

在第一层稳定后，再逐步做到：

1. Chat 交互更像移动聊天界面
2. 导航和侧面板切换更自然
3. 关键管理页也能顺畅操作

### 3.3 当前不追求的目标

第一版不建议追求：

1. 手机上的全功能管理效率对齐桌面
2. 所有页面都做到像原生 App
3. 大型配置编辑在手机上体验优秀
4. 完全重构 DOM 层级

---

## 4. 详细改造策略

## 4.1 总体策略：响应式优先，必要时才补少量状态控制

本方案的默认原则改为：

1. **布局变化优先通过 CSS media query 实现**
2. **不做移动端跳转，不做单独 mobile 路由**
3. **不通过 JS 做 desktop/mobile 两套布局切换**
4. **只有抽屉开合、遮罩显隐这类瞬时 UI 状态，才允许极少量 JS 参与**

建议不是只做一个 mobile breakpoint，而是做三档：

### Desktop：`>= 1200px`

保留当前主体思路：

1. 左侧导航常驻
2. 右侧信息面板常驻
3. 中间主内容区独立滚动

### Tablet：`768px ~ 1199px`

目标：开始压缩框架，但仍保留较强管理能力。

建议：

1. 左侧导航改抽屉或窄栏
2. 右侧信息面板改隐藏式 inspector
3. 主内容区全宽
4. 页面内部卡片与双列布局大多转单列

### Mobile：`< 768px`

目标：围绕 Chat 和基本浏览操作优化。

建议：

1. 顶部显示导航按钮和 inspector 按钮
2. 左侧导航改 overlay drawer
3. 右侧面板改 overlay inspector
4. 主内容区全屏
5. header 动作区允许换行或折叠
6. 复杂编辑区默认单列

---

## 5. 实施规划

## 5.1 Phase 0：先补移动布局基础设施

这是整个项目最关键的一步。

### 目标

建立后续所有移动端样式改造都能依赖的基础框架。

### 需要做的事

#### 1. 在 DOM 中增加尽量少的控制入口

建议在 `web/index.html` 只增加真正必需的元素：

1. sidebar toggle 按钮
2. inspector toggle 按钮
3. overlay 遮罩层

建议新增的 class 也尽量克制：

1. `sidebar-open`
2. `inspector-open`

不建议再引入 `layout-desktop`、`layout-tablet`、`layout-mobile` 这类由 JS 驱动的布局 class。断点本身由 CSS media query 直接控制即可。

### 2. JS 只负责少量瞬时 UI 状态

建议不要在 `web/js/app.js` 里增加基于断点的布局切换逻辑。

更合适的做法是：

1. 断点和布局重排全部交给 CSS
2. JS 只负责 `open/close` 这类瞬时状态
3. 不引入 `syncLayoutMode()`、`handleViewportResize()` 这类以视口判断驱动布局的函数

如果确实需要补 JS，建议只保留：

1. `openSidebar()` / `closeSidebar()`
2. `openInspector()` / `closeInspector()`
3. 点击遮罩关闭
4. 切页后关闭当前 overlay

职责仅限于开关状态，不参与断点判断和布局分支。

### 3. 在 CSS 中改造全局三栏布局

重点文件：

- `web/css/common.css`

建议做法：

1. 把当前固定三栏定义保留为 desktop 基线
2. 新增 tablet/mobile 断点重写 `.sidebar`、`.main-content`、`.right-panel`
3. 手机上把 sidebar / right-panel 变成 overlay drawer
4. 主内容区不再依赖 `calc(100vw - ...)`

### 4. 替换高度策略

当前的 `100vh` 在手机浏览器上不稳。

建议：

1. 桌面端可继续兼容 `100vh`
2. 移动端优先使用 `100dvh`
3. 保留 fallback，避免旧设备异常

### 5. 统一 header 在窄屏下的换行策略

当前很多页面的 `.page-header` + `.page-header-actions` 在手机上一定会拥挤。

相关位置：

- `web/css/common.css:376`
- `web/css/common.css:504`

建议：

1. 手机下 header 改为纵向堆叠
2. 标题在上，操作区在下
3. 操作区允许自动换行
4. 次要 chip 可降低优先级或隐藏

### Phase 0 输出结果

完成后应该做到：

1. 手机打开后，页面不再被左右栏挤爆
2. 可通过按钮打开/关闭导航与 inspector
3. 主内容区可以独立完整展示

---

## 5.2 Phase 1：连接页和全局框架可用

### 目标

先解决进入系统前的第一屏体验。

### 当前问题

连接页：

- `web/css/connection.css:24` 固定 `width: 500px`
- `web/css/connection.css:29` 固定大 padding

手机上会带来：

1. 空间浪费
2. 横向紧张
3. 键盘弹出后内容容易拥挤

### 调整建议

1. 改为 `width: min(500px, calc(100vw - 32px))`
2. 手机下缩小 padding
3. 输入与按钮增大触控高度
4. 连接页容器允许在超小屏上纵向滚动

### 验收

1. 360px 宽屏可正常显示
2. 输入框不会超出容器
3. 键盘弹出时仍能点到 Connect

---

## 5.3 Phase 2：Chat 页移动优先改造

这是业务价值最高的一期。

### 目标

把 Chat 做成手机上真正可用的主路径。

### 当前结构分析

Chat 主结构位于：

- `web/index.html:127`

核心区域：

1. page header
2. chat messages
3. newest button
4. typing indicator
5. chat input

### 问题点

#### 1. 气泡宽度偏保守

- `web/css/chat.css:96` 中 `.message-content` 为 `max-width: 65%`

手机上建议放宽到 88%~92%。

#### 2. 输入区按钮较多

输入区包含：

1. `/` 命令按钮
2. `+` 上传按钮
3. 文本输入框
4. Send/Stop 按钮

在手机横向空间下会非常紧。

#### 3. `Newest` 按钮与输入区可能互相压住

- `web/css/chat.css:10`

当前是绝对定位，手机键盘弹出时容易位置失准。

#### 4. 输入法和 viewport 联动未处理

当前没有 `visualViewport` 或键盘弹出专门处理。

### 调整建议

#### A. Chat header 简化

手机下建议：

1. 标题保留
2. `Back to Main Chat`、`New Session`、`agent selector`、状态标签允许换行
3. 将部分次要动作做成紧凑模式

#### B. Message 区调整

1. 消息 padding 变小
2. 气泡宽度增大
3. 日志类消息字号稍降
4. 媒体缩略图更适应窄屏

#### C. 输入区重排

建议手机下改成两层：

1. 第一层：附件托盘
2. 第二层：命令/上传/输入框/发送按钮

如果仍然拥挤，则进一步改成：

1. 左侧一个“更多”按钮
2. 命令和上传并入弹出菜单
3. 输入框 + 发送按钮保留主位

#### D. 附件托盘竖向卡片化

当前已有上传功能，建议移动端：

1. 每个附件占整行
2. 预览图更大
3. 状态更清晰
4. 删除按钮更易点

#### E. Stop / Send 的手势与点击面积

手机上中断任务是高频关键动作，按钮要足够大，且不要和输入区抢位置。

### 验收

1. 纯文本发送顺畅
2. 图文发送顺畅
3. 任务运行中可点 Stop
4. 键盘弹出时输入区不遮挡
5. 消息列表滚动到底部正常

---

## 5.4 Phase 3：导航与右侧面板移动交互

### 目标

把桌面上的“左右常驻栏”改造成移动下“按需打开的信息层”。

### Sidebar 建议

当前 sidebar 内容较多，适合作为 drawer：

- `web/index.html:34`

建议：

1. 手机下默认隐藏
2. 点击按钮从左侧滑出
3. 点击页面空白关闭
4. 切换页面后自动关闭

这个“切换页面后自动关闭”可以直接接在已有 `nav-item` 点击逻辑后面，改动较集中在：

- `web/js/app.js:41`

### Right panel 建议

当前 right panel 主要是辅助信息：

- Tasks
- Context
- Runtime
- System

相关位置：

- `web/index.html:88`

建议：

1. 桌面常驻
2. tablet/mobile 下通过 inspector 按钮展开
3. 展开后以右侧或全屏 overlay 呈现
4. 内部内容保留原有渲染逻辑，不需要重写

### 验收

1. 打开关闭流畅
2. 遮罩点击可关闭
3. 页面切换不残留 drawer 状态
4. 不影响现有任务/上下文渲染逻辑

---

## 5.5 Phase 4：页面级适配优先级与具体策略

这里按“收益高、改动低、风险小”的顺序排。

### P1：Agents

文件：

- `web/css/agents.css:1`

当前：

- 卡片网格 `minmax(300px, 1fr)`

建议：

1. 手机下改为单列
2. 卡片 padding 缩小
3. avatar 和 header 间距缩小

### P1：Sessions

虽然本次没专门读 `sessions.css`，但从结构上看应作为优先项，因为它是移动端常见入口。

建议：

1. 会话卡片单列
2. 操作按钮换行
3. 会话元信息分段显示

### P1：Skills

文件：

- `web/css/skills.css:1`

当前已有：

- `1100px` 下整体单列
- `640px` 下列表单列

还需要补：

1. preview 面板在手机下默认后置
2. title row 按钮区更紧凑
3. 大段预览文本区域高度控制

### P1：Memory

文件：

- `web/css/memory.css:1`

当前已有 1100px 单列，属于适配基础较好的一页。

需要补：

1. preview panel 手机下减少最小高度
2. 内容区更紧凑
3. 列表项点击热区优化

### P1：Knowledge

文件：

- `web/css/knowledge.css:1`

这是复杂度最高、但移动浏览价值也不低的一页。

当前问题：

1. 多处 grid
2. 搜索栏固定宽度输入框 `300px`，见 `web/css/knowledge.css:390`
3. 编辑器与预览器双栏
4. 文件树和标签筛选同屏

建议：

1. 搜索栏和 toolbar 全部允许换行
2. 编辑器与预览器手机下改成 tab 切换，而不是强行上下同时展示
3. 目录树收进折叠区
4. modal 宽度改成更安全的 `calc(100vw - 24px)`

### P2：Schedules

文件：

- `web/css/schedules.css:1`

当前已有：

- `1180px` 下整体单列

仍需补：

1. 顶部 header actions 换行
2. 表单 label/value 在手机下由横向改纵向
3. schedule action 按钮区避免挤成一团

### P2：Diagnostics

文件：

- `web/css/diagnostics.css:1`

当前已有：

- `1100px` 下主体单列
- `720px` 下表格最小宽度取消

但注意：

- `diagnostics-table-shell` 本身已支持横向滚动，见 `web/css/diagnostics.css:130`

建议：

1. summary card 单列即可
2. 大表格保留横向滚动，不强求全压缩
3. 手机下减少次级统计信息同时显示密度

### P3：Settings

文件：

- `web/css/settings.css:1`

这是手机端最不值得强优化的一页，但至少要保证不坏。

问题：

1. `settings-input` 固定宽 `200px`，见 `web/css/settings.css:161`
2. 多处复杂 grid 和 KV 布局
3. 大编辑器 `min-height: 560px`

建议：

1. 手机下 label/value 全改纵向
2. 输入控件宽度 `100%`
3. YAML/JSON 编辑器降低默认高度
4. `settings-kv-row` 改成单列
5. 明确接受“能看能编辑基础字段，但不追求高效”

### P3：Logs

文件：

- `web/css/logs.css:1`

建议：

1. 顶部 filter 区换行
2. 日志容器最大高度在移动端改为更灵活的剩余高度
3. time/level/header 可堆叠，避免横向拥挤

---

## 5.6 Phase 5：细节打磨与移动专属体验

这一步不是必须首发做，但可以作为第二轮优化。

建议内容：

1. Chat 页在 mobile 下默认 landing 到 chat
2. 低优先级页面显示“建议桌面端使用”轻提示
3. 命令、上传、更多动作合并成一个 mobile action sheet
4. 根据 usage 再决定是否加 `mobile-chat` 模式

注意这里仍然是：

**同一套应用内的 mobile 模式增强，不是独立移动站点。**

---

## 6. 推荐的文件改动清单

### 必改

1. `web/index.html`
2. `web/css/common.css`
3. `web/css/connection.css`
4. `web/css/chat.css`
5. `web/js/app.js`

### 高优先补充

6. `web/css/agents.css`
7. `web/css/sessions.css`
8. `web/css/skills.css`
9. `web/css/memory.css`
10. `web/css/knowledge.css`

### 第二批补充

11. `web/css/schedules.css`
12. `web/css/diagnostics.css`
13. `web/css/settings.css`
14. `web/css/logs.css`
15. 可能少量 `web/js/*-page.js`，用于抽屉开关后的一些收尾行为

---

## 7. 风险与注意点

### 7.1 最大风险是动到全局骨架

因为三栏都依赖 `position: fixed`，一旦改动：

1. 滚动容器边界会变化
2. 遮罩层级会变化
3. 某些绝对定位元素会受影响

其中 Chat 页的：

- `Newest` 按钮
- typing indicator
- 输入区

需要重点回归。

### 7.2 不建议一开始就重构为全新 DOM

当前最稳妥的是：

1. 尽量保留 DOM 结构
2. 通过 class + media query 切换布局
3. 只在必要时增加 toggle button / overlay 容器

这样对现有 JS 影响最小。

### 7.3 移动端测试不能只看 DevTools 缩放

至少要验证：

1. Chrome Android 尺寸
2. iPhone Safari 尺寸
3. 键盘弹出
4. 横竖屏切换
5. 长消息和多附件场景

---

## 8. 建议的实施顺序

如果接下来正式开做，我建议严格按这个顺序：

### Sprint 1：框架与入口

1. 改 `index.html`
2. 改 `common.css`
3. 在 `app.js` 只增加最小化 drawer 开关状态控制
4. 修连接页 `connection.css`

### Sprint 2：Chat 可用

1. 改 `chat.css`
2. 调整 chat header
3. 调整输入区、附件托盘、消息区
4. 处理手机下滚动与按钮位置

### Sprint 3：浏览型页面

1. Agents
2. Sessions
3. Skills
4. Memory
5. Knowledge

### Sprint 4：复杂管理页

1. Schedules
2. Diagnostics
3. Settings
4. Logs

### Sprint 5：体验优化

1. mobile 默认打开 chat
2. 低优先级页面提示
3. 移动交互细节收尾

---

## 9. 验收清单

### 9.1 全局

1. 页面无明显横向滚动
2. drawer 可打开关闭
3. overlay 层级正确
4. 页面切换后状态正常

### 9.2 连接页

1. 小屏可完整显示
2. 键盘弹出不遮挡主要按钮
3. 连接成功后顺利进入 app

### 9.3 Chat

1. 发文本
2. 发图片
3. 图文混发
4. 停止任务
5. 切换 agent
6. 打开旧 session
7. 命令下拉正常
8. 键盘弹起时输入区可见

### 9.4 导航

1. 打开 sidebar
2. 切页自动关闭 sidebar
3. 打开 inspector
4. 点击遮罩关闭 inspector

### 9.5 主要页面

1. Agents 可浏览
2. Sessions 可浏览并进入会话
3. Skills 可浏览与预览
4. Memory 可浏览与预览
5. Knowledge 可查看和基础编辑
6. Settings 至少可查看和改简单字段

---

## 10. 最终建议

针对当前这个项目，我的明确建议是：

1. **继续复用现有 Web。**
2. **移动端改造优先走 CSS 响应式和 media query。**
3. **第一关键点是 `web/css/common.css` 的全局三栏骨架。**
4. **JS 只保留抽屉开合、遮罩显隐这类最小状态控制。**
5. **第二关键点是 Chat 页输入区与 viewport/键盘联动。**
6. **页面级兼容按“浏览型优先，复杂编辑型次之”推进。**
7. **现在不要拆第二套移动站点，否则维护成本会明显高于收益。**

## 11. 实施任务清单

下面这部分不是原则说明，而是可以直接照着推进的开发 checklist。

### 11.1 `web/index.html`

- [ ] 在全局 header 或主内容可见区域补一个 sidebar toggle 入口
- [ ] 在全局 header 或主内容可见区域补一个 inspector toggle 入口
- [ ] 增加统一 overlay 遮罩节点，供 sidebar / inspector 共用
- [ ] 确认新增按钮不会破坏现有 desktop 布局
- [ ] 新增按钮文案或图标保持足够直观，触控面积适合手机点击

### 11.2 `web/css/common.css`

- [ ] 保留 desktop 三栏作为默认基线，不先重写桌面结构
- [ ] 将 `body` / 根容器的 `100vh` 策略改为兼容 `100dvh` 的写法
- [ ] 重新梳理 `.sidebar`、`.main-content`、`.right-panel` 在 tablet/mobile 下的定位方式
- [ ] 去掉 mobile 下对 `.main-content` 的 `calc(100vw - ...)` 依赖
- [ ] 为 mobile/tablet 增加 sidebar overlay 样式
- [ ] 为 mobile/tablet 增加 inspector overlay 样式
- [ ] 补充 overlay 遮罩显隐与层级控制
- [ ] 统一 `.page-header`、`.page-header-actions` 的窄屏换行规则
- [ ] 检查全局滚动容器，避免 body 锁死后主内容无法滚动
- [ ] 检查 z-index，避免抽屉、遮罩、弹窗、下拉互相遮挡

### 11.3 `web/js/app.js`

- [ ] 仅增加最小状态控制：sidebar open / close
- [ ] 仅增加最小状态控制：inspector open / close
- [ ] 支持点击 overlay 关闭当前打开的面板
- [ ] 支持切换页面后自动关闭 sidebar / inspector
- [ ] 不增加 `matchMedia`、`resize` 断点驱动布局逻辑
- [ ] 不引入 desktop/tablet/mobile 三套 JS 布局状态机

### 11.4 `web/css/connection.css`

- [ ] 将固定宽连接框改成 `min(...)` / `calc(...)` 这类响应式宽度
- [ ] 在窄屏下缩小外边距与 padding
- [ ] 提高输入框、按钮的触控高度
- [ ] 允许超小屏或键盘弹出时页面可纵向滚动
- [ ] 确认错误提示、连接状态文案不会把布局撑爆

### 11.5 `web/css/chat.css`

- [ ] 调整消息列表 padding，缩小手机上的左右留白
- [ ] 将 `.message-content` 的 mobile 最大宽度放宽到更适合聊天阅读的范围
- [ ] 检查代码块、表格、长路径、长英文在窄屏下的换行与滚动
- [ ] 重排 chat header，让按钮区在手机下允许换行
- [ ] 重排输入区，优先保证输入框与发送/停止按钮可用
- [ ] 若需要，移动端将命令/上传入口收紧为更紧凑的结构
- [ ] 将附件托盘改成更适合窄屏的竖向卡片样式
- [ ] 检查 `Newest` 按钮与输入区、键盘弹出时的位置关系
- [ ] 检查 typing indicator、上传中状态、任务运行中状态在窄屏下不互相遮挡
- [ ] 检查图片预览、附件卡片、发送失败状态在手机下的表现

### 11.6 `web/css/agents.css`

- [ ] 手机下卡片网格改单列
- [ ] 收紧卡片 padding、标题区间距、次要信息密度

### 11.7 `web/css/sessions.css`

- [ ] 会话列表在手机下改单列
- [ ] 会话操作按钮允许换行或下沉到第二行
- [ ] 元信息避免一行塞满，必要时分块显示

### 11.8 `web/css/skills.css`

- [ ] 继续沿用现有单列断点，并补齐更窄屏下的间距优化
- [ ] preview 区在手机下后置，不抢首屏空间
- [ ] 长文本预览区控制默认高度，避免整页被预览占满

### 11.9 `web/css/memory.css`

- [ ] 在现有单列基础上继续压缩间距与最小高度
- [ ] 优化列表项触控热区
- [ ] 预览区默认高度更适合手机阅读

### 11.10 `web/css/knowledge.css`

- [ ] 搜索栏、筛选区、toolbar 全部允许换行
- [ ] 固定宽输入框改为响应式宽度
- [ ] 编辑器/预览器在手机下不再强行双栏同屏
- [ ] 文件树、标签区、辅助筛选项必要时做折叠
- [ ] modal 宽度与高度改为更安全的窄屏规则

### 11.11 `web/css/schedules.css`

- [ ] 头部操作区允许换行
- [ ] 表单项在手机下尽量从横向改纵向
- [ ] 操作按钮避免并排过多导致误触

### 11.12 `web/css/diagnostics.css`

- [ ] 统计卡片改更稳妥的单列或稀疏双列
- [ ] 大表格优先保留横向滚动，不做过度压缩
- [ ] 次要统计信息降低同屏密度

### 11.13 `web/css/settings.css`

- [ ] 固定宽输入控件改为 `width: 100%` 优先
- [ ] 复杂 grid 和 kv 行在手机下改单列
- [ ] 大编辑器默认高度适当下调
- [ ] 明确以“可用不坏”为第一目标，不追求手机高效编辑复杂配置

### 11.14 `web/css/logs.css`

- [ ] filter 区换行
- [ ] 日志容器高度改成更适配移动端剩余空间的策略
- [ ] 时间、级别、摘要在必要时允许堆叠显示

### 11.15 联调与回归

- [ ] Android Chrome 尺寸验证：至少覆盖 360px 宽度
- [ ] iPhone Safari 尺寸验证：至少覆盖常见窄屏宽度
- [ ] 验证竖屏 / 横屏切换
- [ ] 验证键盘弹出后的聊天输入、发送、停止操作
- [ ] 验证长消息、代码块、图片、附件上传
- [ ] 验证 drawer、overlay、modal、dropdown 的层级不冲突
- [ ] 验证切页后 overlay 状态被正确清理
- [ ] 验证桌面端布局未被响应式改造破坏

### 11.16 推荐落地顺序

1. `web/index.html`
2. `web/css/common.css`
3. `web/js/app.js`
4. `web/css/connection.css`
5. `web/css/chat.css`
6. `web/css/agents.css`
7. `web/css/sessions.css`
8. `web/css/skills.css`
9. `web/css/memory.css`
10. `web/css/knowledge.css`
11. `web/css/schedules.css`
12. `web/css/diagnostics.css`
13. `web/css/settings.css`
14. `web/css/logs.css`
