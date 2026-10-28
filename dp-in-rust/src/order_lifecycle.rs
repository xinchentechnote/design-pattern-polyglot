use std::collections::HashMap;
use std::fmt;

/// 买卖方向（与实战2撮合引擎中的 Side 同义；本模块自成一体，便于单独阅读）。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Side {
    Buy,
    Sell,
}

/// 订单状态（State 模式）：生命周期为
/// Created → Submitted → PartiallyFilled → Filled / Cancelled / Rejected。
/// Rust 用 enum + match 表达状态机：合法转换在代码里显式可见，漏写分支编译不过。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OrderState {
    /// 已创建，尚未提交
    Created,
    /// 已提交，等待撮合
    Submitted,
    /// 部分成交
    PartiallyFilled,
    /// 全部成交（终态）
    Filled,
    /// 已撤销（终态）
    Cancelled,
    /// 风控/交易所拒单（终态）
    Rejected,
}

impl OrderState {
    /// "当前状态允许哪些操作"集中在一处，替代散落各处的 if (status == XXX)
    pub fn is_terminal(self) -> bool {
        match self {
            OrderState::Created | OrderState::Submitted | OrderState::PartiallyFilled => false,
            OrderState::Filled | OrderState::Cancelled | OrderState::Rejected => true,
        }
    }

    pub fn can_submit(self) -> bool {
        match self {
            OrderState::Created => true,
            OrderState::Submitted
            | OrderState::PartiallyFilled
            | OrderState::Filled
            | OrderState::Cancelled
            | OrderState::Rejected => false,
        }
    }

    pub fn can_reject(self) -> bool {
        match self {
            OrderState::Created | OrderState::Submitted => true,
            OrderState::PartiallyFilled
            | OrderState::Filled
            | OrderState::Cancelled
            | OrderState::Rejected => false,
        }
    }

    /// 只有在途订单（已提交/部分成交）可撤：已成交不可撤是显式规则，不是约定
    pub fn can_cancel(self) -> bool {
        match self {
            OrderState::Submitted | OrderState::PartiallyFilled => true,
            OrderState::Created
            | OrderState::Filled
            | OrderState::Cancelled
            | OrderState::Rejected => false,
        }
    }

    /// 在途订单可改单
    pub fn can_modify(self) -> bool {
        self.can_cancel()
    }

    /// 在途订单可接受成交回报
    pub fn can_fill(self) -> bool {
        self.can_cancel()
    }
}

/// 状态机与命令的失败原因：非法转换必须显式拒绝，绝不静默吞掉。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum OrderError {
    /// 当前状态不允许该操作（如 Filled 状态撤单）
    IllegalTransition {
        action: &'static str,
        from: OrderState,
    },
    /// 成交回报超过剩余量（或为 0）
    Overfill { requested: u64, remaining: u64 },
    /// 改单数量不得小于等于已成交量
    ModifyBelowFilled {
        new_quantity: u64,
        filled_quantity: u64,
    },
    /// 订单不存在
    OrderNotFound(u64),
    /// 命令历史为空，无可撤销
    NothingToUndo,
    /// 命令执行后订单又发生了变化，旧快照不能覆盖新事件
    UndoConflict {
        order_id: u64,
        expected_version: u64,
        actual_version: u64,
    },
    /// 快照属于另一张订单，禁止跨订单恢复
    SnapshotOrderMismatch {
        expected_order_id: u64,
        snapshot_order_id: u64,
    },
}

impl fmt::Display for OrderError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            OrderError::IllegalTransition { action, from } => {
                write!(
                    f,
                    "illegal transition: cannot {} in state {:?}",
                    action, from
                )
            }
            OrderError::Overfill {
                requested,
                remaining,
            } => {
                write!(
                    f,
                    "overfill: requested {} but remaining {}",
                    requested, remaining
                )
            }
            OrderError::ModifyBelowFilled {
                new_quantity,
                filled_quantity,
            } => {
                write!(
                    f,
                    "modify below filled: new quantity {} <= filled {}",
                    new_quantity, filled_quantity
                )
            }
            OrderError::OrderNotFound(id) => write!(f, "order not found: {}", id),
            OrderError::NothingToUndo => write!(f, "nothing to undo"),
            OrderError::UndoConflict {
                order_id,
                expected_version,
                actual_version,
            } => write!(
                f,
                "undo conflict: order {} expected version {} but was {}",
                order_id, expected_version, actual_version
            ),
            OrderError::SnapshotOrderMismatch {
                expected_order_id,
                snapshot_order_id,
            } => write!(
                f,
                "snapshot order mismatch: expected order {} but snapshot belongs to {}",
                expected_order_id, snapshot_order_id
            ),
        }
    }
}

/// 订单（原发器，Originator）：状态的唯一权威持有者。
/// `state` 字段私有，外部只能经由 submit / fill / cancel / modify / reject
/// 沿状态机迁移；快照与恢复也由订单自己完成——Memento 不破坏封装的关键。
#[derive(Debug)]
pub struct Order {
    id: u64,
    side: Side,
    price: u64,
    quantity: u64,
    filled_quantity: u64,
    /// 数据版本号：每次迁移 +1，快照会记录（乐观锁 / 日终对账用）
    version: u64,
    state: OrderState,
}

impl Order {
    /// 新建订单：状态 Created，版本 1
    pub fn new(id: u64, side: Side, price: u64, quantity: u64) -> Self {
        Self {
            id,
            side,
            price,
            quantity,
            filled_quantity: 0,
            version: 1,
            state: OrderState::Created,
        }
    }

    pub fn state(&self) -> OrderState {
        self.state
    }

    pub fn id(&self) -> u64 {
        self.id
    }
    pub fn side(&self) -> Side {
        self.side
    }
    pub fn price(&self) -> u64 {
        self.price
    }
    pub fn quantity(&self) -> u64 {
        self.quantity
    }
    pub fn filled_quantity(&self) -> u64 {
        self.filled_quantity
    }
    pub fn version(&self) -> u64 {
        self.version
    }

    /// 剩余数量 = 委托数量 - 已成交量
    pub fn remaining_quantity(&self) -> u64 {
        self.quantity - self.filled_quantity
    }

    /// 提交：Created → Submitted
    pub fn submit(&mut self) -> Result<(), OrderError> {
        if !self.state.can_submit() {
            return Err(OrderError::IllegalTransition {
                action: "submit",
                from: self.state,
            });
        }
        self.state = OrderState::Submitted;
        self.version += 1;
        Ok(())
    }

    /// 拒单回报：Created / Submitted → Rejected（提交前风控拒，或交易所拒单回报）
    pub fn reject(&mut self) -> Result<(), OrderError> {
        if !self.state.can_reject() {
            return Err(OrderError::IllegalTransition {
                action: "reject",
                from: self.state,
            });
        }
        self.state = OrderState::Rejected;
        self.version += 1;
        Ok(())
    }

    /// 成交回报：按成交量推进状态。剩余量不足显式拒绝。
    pub fn fill(&mut self, quantity: u64) -> Result<(), OrderError> {
        if !self.state.can_fill() {
            return Err(OrderError::IllegalTransition {
                action: "fill",
                from: self.state,
            });
        }
        let remaining = self.remaining_quantity();
        if quantity == 0 || quantity > remaining {
            return Err(OrderError::Overfill {
                requested: quantity,
                remaining,
            });
        }
        self.filled_quantity += quantity;
        self.state = if self.filled_quantity == self.quantity {
            OrderState::Filled
        } else {
            OrderState::PartiallyFilled
        };
        self.version += 1;
        Ok(())
    }

    /// 撤单：撤销剩余委托量。已成交/已撤销/已拒的单不可撤。
    pub fn cancel(&mut self) -> Result<(), OrderError> {
        if !self.state.can_cancel() {
            return Err(OrderError::IllegalTransition {
                action: "cancel",
                from: self.state,
            });
        }
        self.state = OrderState::Cancelled;
        self.version += 1;
        Ok(())
    }

    /// 改单（Cancel-Replace 的简化：原单修订价格与数量）。
    /// 新数量必须大于已成交量，否则剩余量为负。
    pub fn modify(&mut self, new_price: u64, new_quantity: u64) -> Result<(), OrderError> {
        if !self.state.can_modify() {
            return Err(OrderError::IllegalTransition {
                action: "modify",
                from: self.state,
            });
        }
        if new_quantity <= self.filled_quantity {
            return Err(OrderError::ModifyBelowFilled {
                new_quantity,
                filled_quantity: self.filled_quantity,
            });
        }
        self.price = new_price;
        self.quantity = new_quantity;
        self.version += 1;
        Ok(())
    }

    /// 拍快照：命令回滚与故障恢复（日终对账）共用同一机制
    pub fn snapshot(&self) -> OrderMemento {
        OrderMemento {
            order_id: self.id,
            state: self.state,
            price: self.price,
            quantity: self.quantity,
            filled_quantity: self.filled_quantity,
            version: self.version,
        }
    }

    /// 恢复快照中的业务字段；恢复本身是一次新变更，所以版本号只增不减。
    fn restore(&mut self, memento: &OrderMemento) -> Result<(), OrderError> {
        if self.id != memento.order_id {
            return Err(OrderError::SnapshotOrderMismatch {
                expected_order_id: self.id,
                snapshot_order_id: memento.order_id,
            });
        }
        self.state = memento.state;
        self.price = memento.price;
        self.quantity = memento.quantity;
        self.filled_quantity = memento.filled_quantity;
        self.version = self.version.max(memento.version) + 1;
        Ok(())
    }
}

/// 订单快照（Memento）：只含数据不含行为，字段私有。
/// 外部（caretaker）只负责存取，既改不了内容，也读不出细节——
/// `summary()` 仅供审计/对账展示。
#[derive(Debug, Clone)]
pub struct OrderMemento {
    order_id: u64,
    state: OrderState,
    price: u64,
    quantity: u64,
    filled_quantity: u64,
    version: u64,
}

impl OrderMemento {
    /// 对账展示用摘要
    pub fn summary(&self) -> String {
        format!(
            "order={} state={:?} price={} qty={} filled={} v{}",
            self.order_id,
            self.state,
            self.price,
            self.quantity,
            self.filled_quantity,
            self.version
        )
    }
}

/// 订单命令（Command 模式）：把撤单/改单从"按钮回调"对象化为
/// 可排队、可审计、可撤销补偿的操作，统一 execute / undo 接口。
/// 并非所有迁移都值得做成命令：成交回报不可撤销，直接走 Order 状态机。
pub trait OrderCommand {
    /// 命令描述（审计日志用）
    fn describe(&self) -> String;
    /// 目标订单 id
    fn order_id(&self) -> u64;
    /// 执行命令。失败时订单保持原状，命令不进入历史。
    fn execute(&mut self, order: &mut Order) -> Result<(), OrderError>;
    /// 撤销补偿：仅当订单仍是命令执行后的版本时恢复，避免覆盖后续事件。
    fn undo(&mut self, order: &mut Order) -> Result<(), OrderError>;
}

/// 撤单命令：execute 前先拍快照（Memento），undo 整单恢复——
/// 不只是把状态改回去，价格、成交进度一并回到撤单前。
pub struct CancelCommand {
    order_id: u64,
    memento: Option<OrderMemento>,
    executed_version: Option<u64>,
}

impl CancelCommand {
    pub fn new(order_id: u64) -> Self {
        Self {
            order_id,
            memento: None,
            executed_version: None,
        }
    }
}

impl OrderCommand for CancelCommand {
    fn describe(&self) -> String {
        format!("CANCEL order={}", self.order_id)
    }

    fn order_id(&self) -> u64 {
        self.order_id
    }

    fn execute(&mut self, order: &mut Order) -> Result<(), OrderError> {
        let memento = order.snapshot();
        order.cancel()?;
        self.memento = Some(memento);
        self.executed_version = Some(order.version());
        Ok(())
    }

    fn undo(&mut self, order: &mut Order) -> Result<(), OrderError> {
        if let (Some(memento), Some(expected_version)) =
            (self.memento.as_ref(), self.executed_version)
        {
            if order.version() != expected_version {
                return Err(OrderError::UndoConflict {
                    order_id: self.order_id,
                    expected_version,
                    actual_version: order.version(),
                });
            }
            order.restore(memento)?;
            self.memento = None;
            self.executed_version = None;
        }
        Ok(())
    }
}

/// 改单命令：记录改单前快照，undo 回滚价格与数量。
/// 真实柜台里改单 = 撤旧单 + 下新单（Cancel-Replace），这里简化为原单修订，
/// 以演示 Command（对象化操作）与 Memento（快照回滚）的闭环。
pub struct ModifyCommand {
    order_id: u64,
    new_price: u64,
    new_quantity: u64,
    memento: Option<OrderMemento>,
    executed_version: Option<u64>,
}

impl ModifyCommand {
    pub fn new(order_id: u64, new_price: u64, new_quantity: u64) -> Self {
        Self {
            order_id,
            new_price,
            new_quantity,
            memento: None,
            executed_version: None,
        }
    }
}

impl OrderCommand for ModifyCommand {
    fn describe(&self) -> String {
        format!(
            "MODIFY order={} to price={} qty={}",
            self.order_id, self.new_price, self.new_quantity
        )
    }

    fn order_id(&self) -> u64 {
        self.order_id
    }

    fn execute(&mut self, order: &mut Order) -> Result<(), OrderError> {
        let memento = order.snapshot();
        order.modify(self.new_price, self.new_quantity)?;
        self.memento = Some(memento);
        self.executed_version = Some(order.version());
        Ok(())
    }

    fn undo(&mut self, order: &mut Order) -> Result<(), OrderError> {
        if let (Some(memento), Some(expected_version)) =
            (self.memento.as_ref(), self.executed_version)
        {
            if order.version() != expected_version {
                return Err(OrderError::UndoConflict {
                    order_id: self.order_id,
                    expected_version,
                    actual_version: order.version(),
                });
            }
            order.restore(memento)?;
            self.memento = None;
            self.executed_version = None;
        }
        Ok(())
    }
}

/// OMS（命令调用者 Invoker + 备忘录管理者 Caretaker）：
/// 持有全部订单；撤单/改单以命令对象进入，成功后入栈；
/// undo_last 只恢复栈顶命令，并清理同订单的旧快照；已进入处理入口的成功与失败均落审计日志，
/// 单调递增的序号串成可追踪的时间线，供故障排查与日终对账。
///
/// 注意：undo 采用快照整体回滚，演示场景下命令执行与撤销之间
/// 不穿插同一订单的成交回报；真实柜台用"补偿命令"（如反向改单）
/// 避免覆盖并发成交，快照回滚则留给故障恢复这种"停下来恢复"的场景。
pub struct Oms {
    orders: HashMap<u64, Order>,
    /// 已执行命令栈：从栈顶 undo；恢复成功会清理同订单的其余旧快照
    history: Vec<Box<dyn OrderCommand>>,
    audit_log: Vec<String>,
    next_order_id: u64,
    /// 操作序号：审计流的时间线
    sequence: u64,
}

impl Default for Oms {
    fn default() -> Self {
        Self::new()
    }
}

impl Oms {
    pub fn new() -> Self {
        Self {
            orders: HashMap::new(),
            history: Vec::new(),
            audit_log: Vec::new(),
            next_order_id: 0,
            sequence: 0,
        }
    }

    /// 新建订单（Created 状态），返回订单 id
    pub fn create(&mut self, side: Side, price: u64, quantity: u64) -> u64 {
        self.next_order_id += 1;
        let id = self.next_order_id;
        let order = Order::new(id, side, price, quantity);
        let side_name = match side {
            Side::Buy => "BUY",
            Side::Sell => "SELL",
        };
        self.log(format!(
            "CREATE order={} side={} price={} qty={}",
            id, side_name, price, quantity
        ));
        self.orders.insert(id, order);
        id
    }

    /// 提交订单：走状态机，不是命令（提交的补偿就是撤单，无需单独 undo）
    pub fn submit(&mut self, order_id: u64) -> Result<(), OrderError> {
        let result = match self.orders.get_mut(&order_id) {
            Some(order) => order.submit(),
            None => Err(OrderError::OrderNotFound(order_id)),
        };
        match &result {
            Ok(()) => self.log(format!("SUBMIT order={}", order_id)),
            Err(err) => self.log(format!("SUBMIT order={} FAILED: {}", order_id, err)),
        }
        result
    }

    /// 拒单回报
    pub fn reject(&mut self, order_id: u64) -> Result<(), OrderError> {
        let result = match self.orders.get_mut(&order_id) {
            Some(order) => order.reject(),
            None => Err(OrderError::OrderNotFound(order_id)),
        };
        match &result {
            Ok(()) => self.log(format!("REJECT order={}", order_id)),
            Err(err) => self.log(format!("REJECT order={} FAILED: {}", order_id, err)),
        }
        result
    }

    /// 成交回报：撮合引擎（实战2）驱动，直接走状态机，不可撤销
    pub fn fill(&mut self, order_id: u64, quantity: u64) -> Result<(), OrderError> {
        let result = match self.orders.get_mut(&order_id) {
            Some(order) => order.fill(quantity),
            None => Err(OrderError::OrderNotFound(order_id)),
        };
        match &result {
            Ok(()) => self.log(format!("FILL order={} qty={}", order_id, quantity)),
            Err(err) => self.log(format!(
                "FILL order={} qty={} FAILED: {}",
                order_id, quantity, err
            )),
        }
        result
    }

    /// 执行命令：成功才入栈；失败也留审计痕迹
    pub fn execute(&mut self, mut command: Box<dyn OrderCommand>) -> Result<(), OrderError> {
        let order_id = command.order_id();
        let description = command.describe();
        let result = match self.orders.get_mut(&order_id) {
            Some(order) => command.execute(order),
            None => Err(OrderError::OrderNotFound(order_id)),
        };
        match result {
            Ok(()) => {
                self.log(format!("EXEC {} -> OK", description));
                self.history.push(command);
            }
            Err(err) => {
                self.log(format!("EXEC {} -> FAILED: {}", description, err));
                return Err(err);
            }
        }
        Ok(())
    }

    /// 撤销最近一条命令：版本冲突时拒绝并保留命令历史，避免覆盖后续事件。
    pub fn undo_last(&mut self) -> Result<(), OrderError> {
        let Some(mut command) = self.history.pop() else {
            let err = OrderError::NothingToUndo;
            self.log(format!("UNDO -> FAILED: {}", err));
            return Err(err);
        };
        let order_id = command.order_id();
        let description = command.describe();
        let result = match self.orders.get_mut(&order_id) {
            Some(order) => command.undo(order),
            None => Err(OrderError::OrderNotFound(order_id)),
        };
        match result {
            Ok(()) => {
                // 快照恢复已创建新时间线：同订单更早的命令快照全部失效。
                self.history.retain(|other| other.order_id() != order_id);
                self.log(format!("UNDO {} -> OK", description));
                Ok(())
            }
            Err(err) => {
                self.log(format!("UNDO {} -> FAILED: {}", description, err));
                self.history.push(command);
                Err(err)
            }
        }
    }

    /// 故障恢复：把订单整单回滚到快照时刻，与命令回滚共用 Memento 机制。
    /// 走审计留痕，不暴露可变引用绕过状态机。
    pub fn restore(&mut self, order_id: u64, memento: &OrderMemento) -> Result<(), OrderError> {
        let result = match self.orders.get_mut(&order_id) {
            Some(order) => order.restore(memento),
            None => Err(OrderError::OrderNotFound(order_id)),
        };
        match result {
            Ok(()) => {
                // checkpoint 恢复后，该订单此前的命令快照全部失效。
                self.history
                    .retain(|command| command.order_id() != order_id);
                self.log(format!(
                    "RESTORE order={} from [{}] -> OK",
                    order_id,
                    memento.summary()
                ));
                Ok(())
            }
            Err(err) => {
                self.log(format!("RESTORE order={} -> FAILED: {}", order_id, err));
                Err(err)
            }
        }
    }

    pub fn order(&self, order_id: u64) -> Option<&Order> {
        self.orders.get(&order_id)
    }

    /// 已执行未撤销的命令数（对账用）
    pub fn history_len(&self) -> usize {
        self.history.len()
    }

    /// 审计日志：seq 单调递增，可用于追踪；并非事件溯源级可重放日志
    pub fn audit_log(&self) -> &[String] {
        &self.audit_log
    }

    fn log(&mut self, event: String) {
        self.sequence += 1;
        self.audit_log
            .push(format!("seq={} {}", self.sequence, event));
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_submit_transitions_created_to_submitted() {
        let mut order = Order::new(1, Side::Buy, 100, 10);
        assert_eq!(order.state(), OrderState::Created);
        order.submit().unwrap();
        assert_eq!(order.state(), OrderState::Submitted);
        // 已提交的单重复提交：非法转换显式拒绝
        let err = order.submit().unwrap_err();
        assert_eq!(
            err,
            OrderError::IllegalTransition {
                action: "submit",
                from: OrderState::Submitted
            }
        );
    }

    #[test]
    fn test_reject_from_created_state() {
        let mut order = Order::new(1, Side::Buy, 100, 10);
        order.reject().unwrap();
        assert_eq!(order.state(), OrderState::Rejected);
        assert!(order.state().is_terminal());
        // 终态不可再撤/改/成交
        assert!(!order.state().can_cancel());
        assert!(!order.state().can_modify());
        assert!(!order.state().can_fill());
    }

    #[test]
    fn test_fill_progresses_to_partially_filled_then_filled() {
        let mut order = Order::new(1, Side::Sell, 101, 10);
        order.submit().unwrap();
        order.fill(3).unwrap();
        assert_eq!(order.state(), OrderState::PartiallyFilled);
        assert_eq!(order.remaining_quantity(), 7);
        order.fill(7).unwrap();
        assert_eq!(order.state(), OrderState::Filled);
        // 超量成交与终态后成交都显式拒绝
        assert_eq!(
            order.fill(1),
            Err(OrderError::IllegalTransition {
                action: "fill",
                from: OrderState::Filled
            })
        );
        let mut order2 = Order::new(2, Side::Sell, 101, 5);
        order2.submit().unwrap();
        assert_eq!(
            order2.fill(6),
            Err(OrderError::Overfill {
                requested: 6,
                remaining: 5
            })
        );
    }

    #[test]
    fn test_cancel_filled_order_rejected() {
        let mut oms = Oms::new();
        let id = oms.create(Side::Buy, 100, 10);
        oms.submit(id).unwrap();
        oms.fill(id, 10).unwrap();

        // 已成交不可撤：显式拒绝，订单保持 Filled
        let err = oms.execute(Box::new(CancelCommand::new(id))).unwrap_err();
        assert_eq!(
            err,
            OrderError::IllegalTransition {
                action: "cancel",
                from: OrderState::Filled
            }
        );
        assert_eq!(oms.order(id).unwrap().state(), OrderState::Filled);
        // 失败的命令不进入历史
        assert_eq!(oms.history_len(), 0);
        assert_eq!(oms.undo_last().unwrap_err(), OrderError::NothingToUndo);
        assert!(oms
            .audit_log()
            .last()
            .unwrap()
            .contains("UNDO -> FAILED: nothing to undo"));
    }

    #[test]
    fn test_cancel_command_moves_to_cancelled() {
        let mut oms = Oms::new();
        let id = oms.create(Side::Buy, 100, 10);
        oms.submit(id).unwrap();
        oms.execute(Box::new(CancelCommand::new(id))).unwrap();
        assert_eq!(oms.order(id).unwrap().state(), OrderState::Cancelled);
        assert_eq!(oms.history_len(), 1);
    }

    #[test]
    fn test_undo_cancel_restores_state_and_fills() {
        let mut oms = Oms::new();
        let id = oms.create(Side::Buy, 100, 10);
        oms.submit(id).unwrap();
        oms.fill(id, 2).unwrap(); // 部分成交后撤单
        oms.execute(Box::new(CancelCommand::new(id))).unwrap();
        assert_eq!(oms.order(id).unwrap().state(), OrderState::Cancelled);

        oms.undo_last().unwrap();
        {
            let order = oms.order(id).unwrap();
            // 快照回滚：不只是状态，成交进度一并恢复
            assert_eq!(order.state(), OrderState::PartiallyFilled);
            assert_eq!(order.filled_quantity(), 2);
            assert_eq!(order.remaining_quantity(), 8);
        }
        // 恢复后可继续成交
        oms.fill(id, 8).unwrap();
        assert_eq!(oms.order(id).unwrap().state(), OrderState::Filled);

        // 历史已空：重复 undo 显式报错
        assert_eq!(oms.undo_last().unwrap_err(), OrderError::NothingToUndo);
    }

    #[test]
    fn test_undo_modify_restores_previous_price_and_quantity() {
        let mut oms = Oms::new();
        let id = oms.create(Side::Buy, 100, 10);
        oms.submit(id).unwrap();
        oms.execute(Box::new(ModifyCommand::new(id, 101, 8)))
            .unwrap();
        oms.execute(Box::new(ModifyCommand::new(id, 102, 9)))
            .unwrap();
        {
            let order = oms.order(id).unwrap();
            assert_eq!(order.price(), 102);
            assert_eq!(order.quantity(), 9);
        }

        oms.undo_last().unwrap();
        let order = oms.order(id).unwrap();
        assert_eq!(order.price(), 101);
        assert_eq!(order.quantity(), 8);
        assert_eq!(order.state(), OrderState::Submitted);
        // 恢复创建了新时间线，同订单更早的快照必须清理，不能继续套用旧版本。
        assert_eq!(oms.history_len(), 0);
        assert_eq!(oms.undo_last().unwrap_err(), OrderError::NothingToUndo);
    }

    #[test]
    fn test_modify_quantity_below_filled_rejected() {
        let mut oms = Oms::new();
        let id = oms.create(Side::Buy, 100, 10);
        oms.submit(id).unwrap();
        oms.fill(id, 5).unwrap();

        // 改单数量必须大于已成交量，否则剩余量为负
        assert_eq!(
            oms.execute(Box::new(ModifyCommand::new(id, 100, 4))),
            Err(OrderError::ModifyBelowFilled {
                new_quantity: 4,
                filled_quantity: 5
            })
        );
        assert_eq!(
            oms.execute(Box::new(ModifyCommand::new(id, 100, 5))),
            Err(OrderError::ModifyBelowFilled {
                new_quantity: 5,
                filled_quantity: 5
            })
        );
        oms.execute(Box::new(ModifyCommand::new(id, 102, 6)))
            .unwrap();
        let order = oms.order(id).unwrap();
        assert_eq!(order.price(), 102);
        assert_eq!(order.remaining_quantity(), 1); // 6 - 5
    }

    #[test]
    fn test_audit_trail_records_execute_undo_and_failures() {
        let mut oms = Oms::new();
        let id = oms.create(Side::Buy, 100, 10); // seq=1
        oms.submit(id).unwrap(); // seq=2
        oms.fill(id, 10).unwrap(); // seq=3
        oms.execute(Box::new(CancelCommand::new(id))).unwrap_err(); // seq=4 失败也留痕
        let id2 = oms.create(Side::Sell, 101, 5); // seq=5
        oms.submit(id2).unwrap(); // seq=6
        oms.execute(Box::new(CancelCommand::new(id2))).unwrap(); // seq=7
        oms.undo_last().unwrap(); // seq=8

        let log = oms.audit_log();
        assert_eq!(log.len(), 8);
        assert!(log[0].contains("seq=1 CREATE order=1"));
        assert!(log[3].contains("seq=4 EXEC CANCEL order=1 -> FAILED"));
        assert!(log[3].contains("cannot cancel in state Filled"));
        assert!(log[6].contains("seq=7 EXEC CANCEL order=2 -> OK"));
        assert!(log[7].contains("seq=8 UNDO CANCEL order=2 -> OK"));
        // 执行到 seq=8 后，订单 2 应为 Submitted（撤单被本地 undo）
        assert_eq!(oms.order(id2).unwrap().state(), OrderState::Submitted);
    }

    #[test]
    fn test_memento_snapshot_and_restore_for_recovery() {
        let mut oms = Oms::new();
        let id = oms.create(Side::Buy, 100, 10);
        oms.submit(id).unwrap();
        oms.fill(id, 3).unwrap();

        // 故障恢复场景：拍下当前快照后系统继续推进，再整单恢复到快照时刻
        let snapshot = oms.order(id).unwrap().snapshot();
        assert!(snapshot.summary().contains("state=PartiallyFilled"));
        assert!(snapshot.summary().contains("filled=3"));

        oms.fill(id, 4).unwrap();
        oms.execute(Box::new(ModifyCommand::new(id, 105, 12)))
            .unwrap();
        assert_eq!(oms.order(id).unwrap().remaining_quantity(), 5); // 12 - 7

        oms.restore(id, &snapshot).unwrap();
        let order = oms.order(id).unwrap();
        assert_eq!(order.state(), OrderState::PartiallyFilled);
        assert_eq!(order.filled_quantity(), 3);
        assert_eq!(order.price(), 100);
        assert_eq!(order.quantity(), 10);
        assert_eq!(order.version(), 6); // v5 上执行 restore，版本只增不减
        assert_eq!(oms.history_len(), 0); // checkpoint 恢复使旧命令快照失效
    }

    #[test]
    fn test_reject_after_partial_fill_is_rejected() {
        let mut order = Order::new(1, Side::Buy, 100, 10);
        order.submit().unwrap();
        order.fill(2).unwrap();
        assert_eq!(
            order.reject(),
            Err(OrderError::IllegalTransition {
                action: "reject",
                from: OrderState::PartiallyFilled,
            })
        );
        assert_eq!(order.state(), OrderState::PartiallyFilled);
    }

    #[test]
    fn test_undo_rejects_version_conflict_without_losing_fill() {
        let mut oms = Oms::new();
        let id = oms.create(Side::Buy, 100, 10);
        oms.submit(id).unwrap();
        oms.execute(Box::new(ModifyCommand::new(id, 101, 12)))
            .unwrap();
        oms.fill(id, 3).unwrap(); // 命令后出现真实成交，旧快照已过期

        let err = oms.undo_last().unwrap_err();
        assert_eq!(
            err,
            OrderError::UndoConflict {
                order_id: id,
                expected_version: 3,
                actual_version: 4
            }
        );
        let order = oms.order(id).unwrap();
        assert_eq!(order.price(), 101);
        assert_eq!(order.quantity(), 12);
        assert_eq!(order.filled_quantity(), 3);
        assert_eq!(order.state(), OrderState::PartiallyFilled);
        assert_eq!(oms.history_len(), 1);
        assert!(oms
            .audit_log()
            .last()
            .unwrap()
            .contains("UNDO MODIFY order=1 to price=101 qty=12 -> FAILED"));
    }
}
