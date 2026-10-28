package behavioral

import (
	"fmt"
)

// OrderState 订单状态（State 模式）：生命周期为
// Created → Submitted → PartiallyFilled → Filled / Cancelled / Rejected。
type OrderState int

const (
	StateCreated         OrderState = iota + 1 // 已创建，尚未提交
	StateSubmitted                             // 已提交，等待撮合
	StatePartiallyFilled                       // 部分成交
	StateFilled                                // 全部成交（终态）
	StateCancelled                             // 已撤销（终态）
	StateRejected                              // 风控/交易所拒单（终态）
)

// String 错误消息 / 审计日志里的状态名（与 Rust Debug 输出一致，跨语言可对照）。
func (s OrderState) String() string {
	switch s {
	case StateCreated:
		return "Created"
	case StateSubmitted:
		return "Submitted"
	case StatePartiallyFilled:
		return "PartiallyFilled"
	case StateFilled:
		return "Filled"
	case StateCancelled:
		return "Cancelled"
	case StateRejected:
		return "Rejected"
	}
	return "Unknown"
}

// IsTerminal "当前状态允许哪些操作"集中在状态自己的方法里，替代散落各处的 if status == XXX。
func (s OrderState) IsTerminal() bool {
	return s == StateFilled || s == StateCancelled || s == StateRejected
}

func (s OrderState) CanSubmit() bool { return s == StateCreated }

func (s OrderState) CanReject() bool { return s == StateCreated || s == StateSubmitted }

// CanCancel 只有在途订单（已提交/部分成交）可撤：已成交不可撤是显式规则，不是约定。
func (s OrderState) CanCancel() bool {
	return s == StateSubmitted || s == StatePartiallyFilled
}

// CanModify 在途订单可改单。
func (s OrderState) CanModify() bool { return s.CanCancel() }

// CanFill 在途订单可接受成交回报。
func (s OrderState) CanFill() bool { return s.CanCancel() }

// OrderErrorReason 状态机与命令的失败原因分类。
type OrderErrorReason int

const (
	ReasonIllegalTransition     OrderErrorReason = iota + 1 // 当前状态不允许该操作（如 Filled 状态撤单）
	ReasonOverfill                                          // 成交回报超过剩余量（或为 0）
	ReasonModifyBelowFilled                                 // 改单数量不得小于等于已成交量
	ReasonOrderNotFound                                     // 订单不存在
	ReasonNothingToUndo                                     // 命令历史为空，无可撤销
	ReasonUndoConflict                                      // 命令后又有新事件，禁止旧快照覆盖
	ReasonSnapshotOrderMismatch                             // 快照属于另一张订单
)

// OrderError 状态机与命令的失败原因：非法转换必须显式拒绝，绝不静默吞掉。
// Go 用 errors.As 提取原因字段按 Reason 分派——对应 Rust 的 Result / Java 的受检异常。
type OrderError struct {
	Reason OrderErrorReason

	Action          string     // IllegalTransition：被拒绝的操作名
	From            OrderState // IllegalTransition：转换发生时的状态
	Request         uint64     // Overfill：回报的成交量
	Have            uint64     // Overfill：回报时的剩余量
	NewQty          uint64     // ModifyBelowFilled：改单后的数量
	Filled          uint64     // ModifyBelowFilled：已成交量
	OrderID         uint64     // OrderNotFound：订单 id
	ExpectedVersion uint64     // UndoConflict：命令执行后的版本
	ActualVersion   uint64     // UndoConflict：当前版本
	SnapshotOrderID uint64     // SnapshotOrderMismatch：快照所属订单
}

func (e *OrderError) Error() string {
	switch e.Reason {
	case ReasonIllegalTransition:
		return fmt.Sprintf("illegal transition: cannot %s in state %s", e.Action, e.From)
	case ReasonOverfill:
		return fmt.Sprintf("overfill: requested %d but remaining %d", e.Request, e.Have)
	case ReasonModifyBelowFilled:
		return fmt.Sprintf("modify below filled: new quantity %d <= filled %d", e.NewQty, e.Filled)
	case ReasonOrderNotFound:
		return fmt.Sprintf("order not found: %d", e.OrderID)
	case ReasonNothingToUndo:
		return "nothing to undo"
	case ReasonUndoConflict:
		return fmt.Sprintf("undo conflict: order %d expected version %d but was %d",
			e.OrderID, e.ExpectedVersion, e.ActualVersion)
	case ReasonSnapshotOrderMismatch:
		return fmt.Sprintf("snapshot order mismatch: expected order %d but snapshot belongs to %d",
			e.OrderID, e.SnapshotOrderID)
	}
	return "unknown order error"
}

func illegalTransitionErr(action string, from OrderState) error {
	return &OrderError{Reason: ReasonIllegalTransition, Action: action, From: from}
}

func overfillErr(requested, remaining uint64) error {
	return &OrderError{Reason: ReasonOverfill, Request: requested, Have: remaining}
}

func modifyBelowFilledErr(newQuantity, filledQuantity uint64) error {
	return &OrderError{Reason: ReasonModifyBelowFilled, NewQty: newQuantity, Filled: filledQuantity}
}

func orderNotFoundErr(orderID uint64) error {
	return &OrderError{Reason: ReasonOrderNotFound, OrderID: orderID}
}

func nothingToUndoErr() error {
	return &OrderError{Reason: ReasonNothingToUndo}
}

func undoConflictErr(orderID, expectedVersion, actualVersion uint64) error {
	return &OrderError{Reason: ReasonUndoConflict, OrderID: orderID,
		ExpectedVersion: expectedVersion, ActualVersion: actualVersion}
}

func snapshotOrderMismatchErr(expectedOrderID, snapshotOrderID uint64) error {
	return &OrderError{Reason: ReasonSnapshotOrderMismatch, OrderID: expectedOrderID,
		SnapshotOrderID: snapshotOrderID}
}

// ErrNothingToUndo 供调用方与哨兵比较的便捷形式。
var ErrNothingToUndo = nothingToUndoErr()

// OrderMemento 订单快照（Memento）：只含数据不含行为，字段不导出。
// 包外（caretaker 一侧）只能经 Summary() 展示——同包可见是 Go 的封装边界，
// 对应其他语言里的 friend（C++）/ 包私有（Java）宽接口。
type OrderMemento struct {
	orderID        uint64
	state          OrderState
	price          uint64
	quantity       uint64
	filledQuantity uint64
	version        uint64
}

// Summary 对账展示用摘要。
func (m OrderMemento) Summary() string {
	return fmt.Sprintf("order=%d state=%s price=%d qty=%d filled=%d v%d",
		m.orderID, m.state, m.price, m.quantity, m.filledQuantity, m.version)
}

// ManagedOrder OMS 管理的订单全生命周期记录（原发器，Originator）：
// 与撮合引擎簿内的 Order 不同，这里带状态机。
// state 字段不导出，外部只能经由 Submit / Fill / Cancel / Modify / Reject
// 沿状态机迁移；快照与恢复也由订单自己完成——Memento 不破坏封装的关键。
type ManagedOrder struct {
	id             uint64
	side           Side
	price          uint64
	quantity       uint64
	filledQuantity uint64
	// version 数据版本号：每次迁移 +1，快照会记录（乐观锁 / 日终对账用）
	version uint64
	state   OrderState
}

// NewManagedOrder 新建订单：状态 Created，版本 1。
func NewManagedOrder(id uint64, side Side, price, quantity uint64) *ManagedOrder {
	return &ManagedOrder{id: id, side: side, price: price, quantity: quantity,
		version: 1, state: StateCreated}
}

func (o *ManagedOrder) ID() uint64             { return o.id }
func (o *ManagedOrder) Side() Side             { return o.side }
func (o *ManagedOrder) Price() uint64          { return o.price }
func (o *ManagedOrder) Quantity() uint64       { return o.quantity }
func (o *ManagedOrder) FilledQuantity() uint64 { return o.filledQuantity }
func (o *ManagedOrder) Version() uint64        { return o.version }
func (o *ManagedOrder) State() OrderState      { return o.state }

// RemainingQuantity 剩余数量 = 委托数量 - 已成交量。
func (o *ManagedOrder) RemainingQuantity() uint64 { return o.quantity - o.filledQuantity }

// Submit 提交：Created → Submitted。
func (o *ManagedOrder) Submit() error {
	if !o.state.CanSubmit() {
		return illegalTransitionErr("submit", o.state)
	}
	o.state = StateSubmitted
	o.version++
	return nil
}

// Reject 拒单回报：Created / Submitted → Rejected（提交前风控拒，或交易所拒单回报）。
func (o *ManagedOrder) Reject() error {
	if !o.state.CanReject() {
		return illegalTransitionErr("reject", o.state)
	}
	o.state = StateRejected
	o.version++
	return nil
}

// Fill 成交回报：按成交量推进状态。剩余量不足显式拒绝。
func (o *ManagedOrder) Fill(quantity uint64) error {
	if !o.state.CanFill() {
		return illegalTransitionErr("fill", o.state)
	}
	remaining := o.RemainingQuantity()
	if quantity == 0 || quantity > remaining {
		return overfillErr(quantity, remaining)
	}
	o.filledQuantity += quantity
	if o.filledQuantity == o.quantity {
		o.state = StateFilled
	} else {
		o.state = StatePartiallyFilled
	}
	o.version++
	return nil
}

// Cancel 撤单：撤销剩余委托量。已成交/已撤销/已拒的单不可撤。
func (o *ManagedOrder) Cancel() error {
	if !o.state.CanCancel() {
		return illegalTransitionErr("cancel", o.state)
	}
	o.state = StateCancelled
	o.version++
	return nil
}

// Modify 改单（Cancel-Replace 的简化：原单修订价格与数量）。
// 新数量必须大于已成交量，否则剩余量为负。
func (o *ManagedOrder) Modify(newPrice, newQuantity uint64) error {
	if !o.state.CanModify() {
		return illegalTransitionErr("modify", o.state)
	}
	if newQuantity <= o.filledQuantity {
		return modifyBelowFilledErr(newQuantity, o.filledQuantity)
	}
	o.price = newPrice
	o.quantity = newQuantity
	o.version++
	return nil
}

// Snapshot 拍快照：命令回滚与故障恢复（日终对账）共用同一机制。
func (o *ManagedOrder) Snapshot() OrderMemento {
	return OrderMemento{
		orderID:        o.id,
		state:          o.state,
		price:          o.price,
		quantity:       o.quantity,
		filledQuantity: o.filledQuantity,
		version:        o.version,
	}
}

// Restore 恢复快照中的业务字段；恢复本身是一次新变更，所以版本号只增不减。
func (o *ManagedOrder) restore(m OrderMemento) error {
	if o.id != m.orderID {
		return snapshotOrderMismatchErr(o.id, m.orderID)
	}
	o.state = m.state
	o.price = m.price
	o.quantity = m.quantity
	o.filledQuantity = m.filledQuantity
	o.version = max(o.version, m.version) + 1
	return nil
}

// OrderCommand 订单命令（Command 模式）：把撤单/改单从"按钮回调"对象化为
// 可排队、可审计、可撤销补偿的操作，统一 Execute / Undo 接口。
// 并非所有迁移都值得做成命令：成交回报不可撤销，直接走状态机。
type OrderCommand interface {
	// Describe 命令描述（审计日志用）。
	Describe() string
	// OrderID 目标订单 id。
	OrderID() uint64
	// Execute 执行命令。失败时订单保持原状，命令不进入历史。
	Execute(order *ManagedOrder) error
	// Undo 仅当订单仍是命令执行后的版本时恢复，避免覆盖后续事件。
	Undo(order *ManagedOrder) error
}

// CancelCommand 撤单命令：Execute 前先拍快照（Memento），Undo 整单恢复——
// 不只是把状态改回去，价格、成交进度一并回到撤单前。
type CancelCommand struct {
	orderID         uint64
	memento         *OrderMemento
	executedVersion uint64
}

func NewCancelCommand(orderID uint64) *CancelCommand {
	return &CancelCommand{orderID: orderID}
}

func (c *CancelCommand) Describe() string { return fmt.Sprintf("CANCEL order=%d", c.orderID) }
func (c *CancelCommand) OrderID() uint64  { return c.orderID }

func (c *CancelCommand) Execute(order *ManagedOrder) error {
	snapshot := order.Snapshot()
	if err := order.Cancel(); err != nil {
		return err
	}
	c.memento = &snapshot
	c.executedVersion = order.Version()
	return nil
}

func (c *CancelCommand) Undo(order *ManagedOrder) error {
	if c.memento == nil {
		return nil
	}
	if order.Version() != c.executedVersion {
		return undoConflictErr(c.orderID, c.executedVersion, order.Version())
	}
	if err := order.restore(*c.memento); err != nil {
		return err
	}
	c.memento = nil
	c.executedVersion = 0
	return nil
}

// ModifyCommand 改单命令：记录改单前快照，Undo 回滚价格与数量。
// 真实柜台里改单 = 撤旧单 + 下新单（Cancel-Replace），这里简化为原单修订，
// 以演示 Command（对象化操作）与 Memento（快照回滚）的闭环。
type ModifyCommand struct {
	orderID         uint64
	newPrice        uint64
	newQuantity     uint64
	memento         *OrderMemento
	executedVersion uint64
}

func NewModifyCommand(orderID, newPrice, newQuantity uint64) *ModifyCommand {
	return &ModifyCommand{orderID: orderID, newPrice: newPrice, newQuantity: newQuantity}
}

func (c *ModifyCommand) Describe() string {
	return fmt.Sprintf("MODIFY order=%d to price=%d qty=%d", c.orderID, c.newPrice, c.newQuantity)
}
func (c *ModifyCommand) OrderID() uint64 { return c.orderID }

func (c *ModifyCommand) Execute(order *ManagedOrder) error {
	snapshot := order.Snapshot()
	if err := order.Modify(c.newPrice, c.newQuantity); err != nil {
		return err
	}
	c.memento = &snapshot
	c.executedVersion = order.Version()
	return nil
}

func (c *ModifyCommand) Undo(order *ManagedOrder) error {
	if c.memento == nil {
		return nil
	}
	if order.Version() != c.executedVersion {
		return undoConflictErr(c.orderID, c.executedVersion, order.Version())
	}
	if err := order.restore(*c.memento); err != nil {
		return err
	}
	c.memento = nil
	c.executedVersion = 0
	return nil
}

// OMS（命令调用者 Invoker + 备忘录管理者 Caretaker）：
// 持有全部订单；撤单/改单以命令对象进入，成功后入栈；
// UndoLast 只恢复栈顶命令，并清理同订单的旧快照；已进入处理入口的成功与失败均落审计日志，
// 单调递增的序号串成可追踪的时间线，供故障排查与日终对账。
//
// 注意：undo 采用快照整体回滚，演示场景下命令执行与撤销之间
// 不穿插同一订单的成交回报；真实柜台用"补偿命令"（如反向改单）
// 避免覆盖并发成交，快照回滚则留给故障恢复这种"停下来恢复"的场景。
type Oms struct {
	orders      map[uint64]*ManagedOrder
	history     []OrderCommand
	auditLog    []string
	nextOrderID uint64
	// sequence 操作序号：审计流的时间线
	sequence uint64
}

func NewOms() *Oms {
	return &Oms{orders: make(map[uint64]*ManagedOrder)}
}

// Create 新建订单（Created 状态），返回订单 id。
func (o *Oms) Create(side Side, price, quantity uint64) uint64 {
	o.nextOrderID++
	id := o.nextOrderID
	o.orders[id] = NewManagedOrder(id, side, price, quantity)
	o.log(fmt.Sprintf("CREATE order=%d side=%s price=%d qty=%d", id, side, price, quantity))
	return id
}

// Submit 提交订单：走状态机，不是命令（提交的补偿就是撤单，无需单独 undo）。
func (o *Oms) Submit(orderID uint64) error {
	order, err := o.orderOrErr(orderID)
	if err != nil {
		o.log(fmt.Sprintf("SUBMIT order=%d FAILED: %s", orderID, err))
		return err
	}
	if err := order.Submit(); err != nil {
		o.log(fmt.Sprintf("SUBMIT order=%d FAILED: %s", orderID, err))
		return err
	}
	o.log(fmt.Sprintf("SUBMIT order=%d", orderID))
	return nil
}

// Reject 拒单回报。
func (o *Oms) Reject(orderID uint64) error {
	order, err := o.orderOrErr(orderID)
	if err != nil {
		o.log(fmt.Sprintf("REJECT order=%d FAILED: %s", orderID, err))
		return err
	}
	if err := order.Reject(); err != nil {
		o.log(fmt.Sprintf("REJECT order=%d FAILED: %s", orderID, err))
		return err
	}
	o.log(fmt.Sprintf("REJECT order=%d", orderID))
	return nil
}

// Fill 成交回报：撮合引擎（实战2）驱动，直接走状态机，不可撤销。
func (o *Oms) Fill(orderID, quantity uint64) error {
	order, err := o.orderOrErr(orderID)
	if err != nil {
		o.log(fmt.Sprintf("FILL order=%d qty=%d FAILED: %s", orderID, quantity, err))
		return err
	}
	if err := order.Fill(quantity); err != nil {
		o.log(fmt.Sprintf("FILL order=%d qty=%d FAILED: %s", orderID, quantity, err))
		return err
	}
	o.log(fmt.Sprintf("FILL order=%d qty=%d", orderID, quantity))
	return nil
}

// Execute 执行命令：成功才入栈；失败也留审计痕迹。
func (o *Oms) Execute(command OrderCommand) error {
	description := command.Describe()
	order, err := o.orderOrErr(command.OrderID())
	if err != nil {
		o.log(fmt.Sprintf("EXEC %s -> FAILED: %s", description, err))
		return err
	}
	if err := command.Execute(order); err != nil {
		o.log(fmt.Sprintf("EXEC %s -> FAILED: %s", description, err))
		return err
	}
	o.log(fmt.Sprintf("EXEC %s -> OK", description))
	o.history = append(o.history, command)
	return nil
}

// UndoLast 撤销最近一条命令：版本冲突时拒绝并保留命令历史。
func (o *Oms) UndoLast() error {
	if len(o.history) == 0 {
		o.log(fmt.Sprintf("UNDO -> FAILED: %s", ErrNothingToUndo))
		return ErrNothingToUndo
	}
	command := o.history[len(o.history)-1]
	order, err := o.orderOrErr(command.OrderID())
	if err != nil {
		o.log(fmt.Sprintf("UNDO %s -> FAILED: %s", command.Describe(), err))
		return err
	}
	if err := command.Undo(order); err != nil {
		o.log(fmt.Sprintf("UNDO %s -> FAILED: %s", command.Describe(), err))
		return err
	}
	// 快照恢复已创建新时间线：同订单更早的命令快照全部失效。
	kept := o.history[:0]
	for _, other := range o.history {
		if other.OrderID() != command.OrderID() {
			kept = append(kept, other)
		}
	}
	o.history = kept
	o.log(fmt.Sprintf("UNDO %s -> OK", command.Describe()))
	return nil
}

// Restore 故障恢复：把订单整单回滚到快照时刻，与命令回滚共用 Memento 机制。
// 走审计留痕，不暴露可变引用绕过状态机。
func (o *Oms) Restore(orderID uint64, memento OrderMemento) error {
	order, err := o.orderOrErr(orderID)
	if err != nil {
		o.log(fmt.Sprintf("RESTORE order=%d -> FAILED: %s", orderID, err))
		return err
	}
	if err := order.restore(memento); err != nil {
		o.log(fmt.Sprintf("RESTORE order=%d -> FAILED: %s", orderID, err))
		return err
	}
	kept := o.history[:0]
	for _, command := range o.history {
		if command.OrderID() != orderID {
			kept = append(kept, command)
		}
	}
	o.history = kept
	o.log(fmt.Sprintf("RESTORE order=%d from [%s] -> OK", orderID, memento.Summary()))
	return nil
}

// Order 返回订单的只读快照副本（nil 表示不存在），避免调用方绕过 OMS 审计修改内部状态。
func (o *Oms) Order(orderID uint64) *ManagedOrder {
	order := o.orders[orderID]
	if order == nil {
		return nil
	}
	copy := *order
	return &copy
}

// HistoryLen 已执行未撤销的命令数（对账用）。
func (o *Oms) HistoryLen() int { return len(o.history) }

// AuditLog 审计日志：seq 单调递增，可用于追踪；并非事件溯源级可重放日志。
func (o *Oms) AuditLog() []string { return append([]string(nil), o.auditLog...) }

func (o *Oms) orderOrErr(orderID uint64) (*ManagedOrder, error) {
	order, ok := o.orders[orderID]
	if !ok {
		return nil, orderNotFoundErr(orderID)
	}
	return order, nil
}

func (o *Oms) log(event string) {
	o.sequence++
	o.auditLog = append(o.auditLog, fmt.Sprintf("seq=%d %s", o.sequence, event))
}

// 编译期保证命令实现完整
var (
	_ OrderCommand = (*CancelCommand)(nil)
	_ OrderCommand = (*ModifyCommand)(nil)
	_ error        = (*OrderError)(nil)
)
