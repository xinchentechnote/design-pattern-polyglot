"""订单生命周期 OMS：状态 + 命令 + 备忘录（实战3 Python 版）。"""

from __future__ import annotations

from abc import ABC, abstractmethod
from copy import copy
from dataclasses import dataclass
from enum import Enum, auto


class Side(Enum):
    """买卖方向（与实战2撮合引擎中的 Side 同义；模块自成一体，便于单独阅读）。"""

    BUY = "BUY"
    SELL = "SELL"


class OrderState(Enum):
    """订单状态（State 模式）：生命周期为
    Created → Submitted → PartiallyFilled → Filled / Cancelled / Rejected。

    "当前状态允许哪些操作"集中为枚举的 property，替代散落各处的 if status == XXX；
    value 即状态名，错误消息 / 审计日志与其他语言逐字对齐。
    """

    CREATED = "Created"
    SUBMITTED = "Submitted"
    PARTIALLY_FILLED = "PartiallyFilled"
    FILLED = "Filled"
    CANCELLED = "Cancelled"
    REJECTED = "Rejected"

    @property
    def is_terminal(self) -> bool:
        return self in (OrderState.FILLED, OrderState.CANCELLED, OrderState.REJECTED)

    @property
    def can_submit(self) -> bool:
        return self is OrderState.CREATED

    @property
    def can_reject(self) -> bool:
        return self in (OrderState.CREATED, OrderState.SUBMITTED)

    @property
    def can_cancel(self) -> bool:
        # 只有在途订单（已提交/部分成交）可撤：已成交不可撤是显式规则，不是约定
        return self in (OrderState.SUBMITTED, OrderState.PARTIALLY_FILLED)

    @property
    def can_modify(self) -> bool:
        return self.can_cancel

    @property
    def can_fill(self) -> bool:
        return self.can_cancel


class OrderErrorReason(Enum):
    """状态机与命令的失败原因分类。"""

    ILLEGAL_TRANSITION = auto()  # 当前状态不允许该操作（如 Filled 状态撤单）
    OVERFILL = auto()  # 成交回报超过剩余量（或为 0）
    MODIFY_BELOW_FILLED = auto()  # 改单数量不得小于等于已成交量
    ORDER_NOT_FOUND = auto()  # 订单不存在
    NOTHING_TO_UNDO = auto()  # 命令历史为空，无可撤销
    UNDO_CONFLICT = auto()  # 命令后又有新事件，禁止旧快照覆盖
    SNAPSHOT_ORDER_MISMATCH = auto()  # 快照属于另一张订单


class OrderError(Exception):
    """状态机与命令的失败原因：非法转换必须显式拒绝，绝不静默吞掉。

    Python 无类型化的受检异常，用 reason 字段让调用方按原因分派——
    对应 Rust 的 Result / Java 的受检异常 / Go 的 errors.As。
    """

    def __init__(
        self,
        reason: OrderErrorReason,
        message: str,
        *,
        action: str | None = None,
        from_state: OrderState | None = None,
        requested: int = 0,
        remaining: int = 0,
        new_quantity: int = 0,
        filled_quantity: int = 0,
        order_id: int = 0,
        expected_version: int = 0,
        actual_version: int = 0,
        snapshot_order_id: int = 0,
    ) -> None:
        super().__init__(message)
        self.reason = reason
        self.action = action
        self.from_state = from_state
        self.requested = requested
        self.remaining = remaining
        self.new_quantity = new_quantity
        self.filled_quantity = filled_quantity
        self.order_id = order_id
        self.expected_version = expected_version
        self.actual_version = actual_version
        self.snapshot_order_id = snapshot_order_id

    @classmethod
    def illegal_transition(cls, action: str, from_state: OrderState) -> OrderError:
        return cls(
            OrderErrorReason.ILLEGAL_TRANSITION,
            f"illegal transition: cannot {action} in state {from_state.value}",
            action=action,
            from_state=from_state,
        )

    @classmethod
    def overfill(cls, requested: int, remaining: int) -> OrderError:
        return cls(
            OrderErrorReason.OVERFILL,
            f"overfill: requested {requested} but remaining {remaining}",
            requested=requested,
            remaining=remaining,
        )

    @classmethod
    def modify_below_filled(cls, new_quantity: int, filled_quantity: int) -> OrderError:
        return cls(
            OrderErrorReason.MODIFY_BELOW_FILLED,
            f"modify below filled: new quantity {new_quantity} <= filled {filled_quantity}",
            new_quantity=new_quantity,
            filled_quantity=filled_quantity,
        )

    @classmethod
    def order_not_found(cls, order_id: int) -> OrderError:
        return cls(
            OrderErrorReason.ORDER_NOT_FOUND,
            f"order not found: {order_id}",
            order_id=order_id,
        )

    @classmethod
    def nothing_to_undo(cls) -> OrderError:
        return cls(OrderErrorReason.NOTHING_TO_UNDO, "nothing to undo")

    @classmethod
    def undo_conflict(cls, order_id: int, expected_version: int, actual_version: int) -> OrderError:
        return cls(
            OrderErrorReason.UNDO_CONFLICT,
            f"undo conflict: order {order_id} expected version {expected_version} but was {actual_version}",
            order_id=order_id,
            expected_version=expected_version,
            actual_version=actual_version,
        )

    @classmethod
    def snapshot_order_mismatch(cls, expected_order_id: int, snapshot_order_id: int) -> OrderError:
        return cls(
            OrderErrorReason.SNAPSHOT_ORDER_MISMATCH,
            f"snapshot order mismatch: expected order {expected_order_id} "
            f"but snapshot belongs to {snapshot_order_id}",
            order_id=expected_order_id,
            snapshot_order_id=snapshot_order_id,
        )


@dataclass(frozen=True)
class OrderMemento:
    """订单快照（Memento）：只含数据不含行为。

    frozen=True：Python 版的"不破坏封装"靠不可变类型——快照拍下后
    任何人都改不了，管理者（caretaker）只负责存取与 summary() 展示。
    """

    order_id: int
    state: OrderState
    price: int
    quantity: int
    filled_quantity: int
    version: int

    def summary(self) -> str:
        return (
            f"order={self.order_id} state={self.state.value} price={self.price}"
            f" qty={self.quantity} filled={self.filled_quantity} v{self.version}"
        )


class Order:
    """订单（原发器，Originator）：状态的唯一权威持有者。

    _state 以下划线约定私有，外部只能经由 submit / fill / cancel / modify / reject
    沿状态机迁移；快照与恢复也由订单自己完成——Memento 不破坏封装的关键。
    """

    def __init__(self, order_id: int, side: Side, price: int, quantity: int) -> None:
        self._id = order_id
        self._side = side
        self._price = price
        self._quantity = quantity
        self._filled_quantity = 0
        # 数据版本号：每次迁移 +1，快照会记录（乐观锁 / 日终对账用）
        self._version = 1
        self._state = OrderState.CREATED

    @property
    def state(self) -> OrderState:
        return self._state

    @property
    def id(self) -> int:
        return self._id

    @property
    def side(self) -> Side:
        return self._side

    @property
    def price(self) -> int:
        return self._price

    @property
    def quantity(self) -> int:
        return self._quantity

    @property
    def filled_quantity(self) -> int:
        return self._filled_quantity

    @property
    def version(self) -> int:
        return self._version

    @property
    def remaining_quantity(self) -> int:
        """剩余数量 = 委托数量 - 已成交量。"""
        return self._quantity - self._filled_quantity

    def submit(self) -> None:
        """提交：Created → Submitted。"""
        if not self._state.can_submit:
            raise OrderError.illegal_transition("submit", self._state)
        self._state = OrderState.SUBMITTED
        self._version += 1

    def reject(self) -> None:
        """拒单回报：Created / Submitted → Rejected（提交前风控拒，或交易所拒单回报）。"""
        if not self._state.can_reject:
            raise OrderError.illegal_transition("reject", self._state)
        self._state = OrderState.REJECTED
        self._version += 1

    def fill(self, quantity: int) -> None:
        """成交回报：按成交量推进状态。剩余量不足显式拒绝。"""
        if not self._state.can_fill:
            raise OrderError.illegal_transition("fill", self._state)
        remaining = self.remaining_quantity
        if quantity <= 0 or quantity > remaining:
            raise OrderError.overfill(quantity, remaining)
        self._filled_quantity += quantity
        self._state = (
            OrderState.FILLED
            if self._filled_quantity == self._quantity
            else OrderState.PARTIALLY_FILLED
        )
        self._version += 1

    def cancel(self) -> None:
        """撤单：撤销剩余委托量。已成交/已撤销/已拒的单不可撤。"""
        if not self._state.can_cancel:
            raise OrderError.illegal_transition("cancel", self._state)
        self._state = OrderState.CANCELLED
        self._version += 1

    def modify(self, new_price: int, new_quantity: int) -> None:
        """改单（Cancel-Replace 的简化：原单修订价格与数量）。

        新数量必须大于已成交量，否则剩余量为负。
        """
        if not self._state.can_modify:
            raise OrderError.illegal_transition("modify", self._state)
        if new_quantity <= self._filled_quantity:
            raise OrderError.modify_below_filled(new_quantity, self._filled_quantity)
        self._price = new_price
        self._quantity = new_quantity
        self._version += 1

    def snapshot(self) -> OrderMemento:
        """拍快照：命令回滚与故障恢复（日终对账）共用同一机制。"""
        return OrderMemento(
            order_id=self._id,
            state=self._state,
            price=self._price,
            quantity=self._quantity,
            filled_quantity=self._filled_quantity,
            version=self._version,
        )

    def _restore(self, memento: OrderMemento) -> None:
        """恢复快照中的业务字段；恢复本身是一次新变更，所以版本号只增不减。"""
        if self._id != memento.order_id:
            raise OrderError.snapshot_order_mismatch(self._id, memento.order_id)
        self._state = memento.state
        self._price = memento.price
        self._quantity = memento.quantity
        self._filled_quantity = memento.filled_quantity
        self._version = max(self._version, memento.version) + 1


class OrderCommand(ABC):
    """订单命令（Command 模式）：把撤单/改单从"按钮回调"对象化为
    可排队、可审计、可撤销补偿的操作，统一 execute / undo 接口。

    并非所有迁移都值得做成命令：成交回报不可撤销，直接走 Order 状态机。
    """

    @abstractmethod
    def describe(self) -> str:
        """命令描述（审计日志用）。"""
        ...

    @abstractmethod
    def order_id(self) -> int:
        """目标订单 id。"""
        ...

    @abstractmethod
    def execute(self, order: Order) -> None:
        """执行命令。失败时订单保持原状，命令不进入历史。"""
        ...

    @abstractmethod
    def undo(self, order: Order) -> None:
        """仅当订单仍是命令执行后的版本时恢复，避免覆盖后续事件。"""
        ...


class CancelCommand(OrderCommand):
    """撤单命令：先拍快照（Memento），undo 整单恢复——
    不只是把状态改回去，价格、成交进度一并回到撤单前。"""

    def __init__(self, order_id: int) -> None:
        self._order_id = order_id
        self._memento: OrderMemento | None = None
        self._executed_version: int | None = None

    def describe(self) -> str:
        return f"CANCEL order={self._order_id}"

    def order_id(self) -> int:
        return self._order_id

    def execute(self, order: Order) -> None:
        memento = order.snapshot()  # 先拍快照再迁移：迁移失败则快照丢弃（命令不入历史）
        order.cancel()
        self._memento = memento
        self._executed_version = order.version

    def undo(self, order: Order) -> None:
        if self._memento is not None:
            if order.version != self._executed_version:
                raise OrderError.undo_conflict(
                    self._order_id, self._executed_version or 0, order.version
                )
            order._restore(self._memento)
            self._memento = None
            self._executed_version = None


class ModifyCommand(OrderCommand):
    """改单命令：记录改单前快照，undo 回滚价格与数量。

    真实柜台里改单 = 撤旧单 + 下新单（Cancel-Replace），这里简化为原单修订，
    以演示 Command（对象化操作）与 Memento（快照回滚）的闭环。
    """

    def __init__(self, order_id: int, new_price: int, new_quantity: int) -> None:
        self._order_id = order_id
        self._new_price = new_price
        self._new_quantity = new_quantity
        self._memento: OrderMemento | None = None
        self._executed_version: int | None = None

    def describe(self) -> str:
        return f"MODIFY order={self._order_id} to price={self._new_price} qty={self._new_quantity}"

    def order_id(self) -> int:
        return self._order_id

    def execute(self, order: Order) -> None:
        memento = order.snapshot()
        order.modify(self._new_price, self._new_quantity)
        self._memento = memento
        self._executed_version = order.version

    def undo(self, order: Order) -> None:
        if self._memento is not None:
            if order.version != self._executed_version:
                raise OrderError.undo_conflict(
                    self._order_id, self._executed_version or 0, order.version
                )
            order._restore(self._memento)
            self._memento = None
            self._executed_version = None


class Oms:
    """OMS（命令调用者 Invoker + 备忘录管理者 Caretaker）：
    持有全部订单；撤单/改单以命令对象进入，成功后入栈；
    undo_last 只恢复栈顶命令，并清理同订单的旧快照；已进入处理入口的成功与失败均落审计日志，
    单调递增的序号串成可追踪的时间线，供故障排查与日终对账。

    注意：undo 采用快照整体回滚，演示场景下命令执行与撤销之间
    不穿插同一订单的成交回报；真实柜台用"补偿命令"（如反向改单）
    避免覆盖并发成交，快照回滚则留给故障恢复这种"停下来恢复"的场景。
    """

    def __init__(self) -> None:
        self._orders: dict[int, Order] = {}
        # 已执行命令栈：从栈顶 undo；恢复成功会清理同订单的其余旧快照
        self._history: list[OrderCommand] = []
        self._audit_log: list[str] = []
        self._next_order_id = 0
        self._sequence = 0  # 操作序号：审计流的时间线

    def create(self, side: Side, price: int, quantity: int) -> int:
        """新建订单（Created 状态），返回订单 id。"""
        self._next_order_id += 1
        order_id = self._next_order_id
        self._orders[order_id] = Order(order_id, side, price, quantity)
        self._log(f"CREATE order={order_id} side={side.value} price={price} qty={quantity}")
        return order_id

    def submit(self, order_id: int) -> None:
        """提交订单：走状态机，不是命令（提交的补偿就是撤单，无需单独 undo）。"""
        try:
            order = self._order_or_raise(order_id)
            order.submit()
        except OrderError as err:
            self._log(f"SUBMIT order={order_id} FAILED: {err}")
            raise
        self._log(f"SUBMIT order={order_id}")

    def reject(self, order_id: int) -> None:
        """拒单回报。"""
        try:
            order = self._order_or_raise(order_id)
            order.reject()
        except OrderError as err:
            self._log(f"REJECT order={order_id} FAILED: {err}")
            raise
        self._log(f"REJECT order={order_id}")

    def fill(self, order_id: int, quantity: int) -> None:
        """成交回报：撮合引擎（实战2）驱动，直接走状态机，不可撤销。"""
        try:
            order = self._order_or_raise(order_id)
            order.fill(quantity)
        except OrderError as err:
            self._log(f"FILL order={order_id} qty={quantity} FAILED: {err}")
            raise
        self._log(f"FILL order={order_id} qty={quantity}")

    def execute(self, command: OrderCommand) -> None:
        """执行命令：成功才入栈；失败也留审计痕迹。"""
        description = command.describe()
        try:
            order = self._order_or_raise(command.order_id())
            command.execute(order)
        except OrderError as err:
            self._log(f"EXEC {description} -> FAILED: {err}")
            raise
        self._log(f"EXEC {description} -> OK")
        self._history.append(command)

    def undo_last(self) -> None:
        """撤销最近一条命令：栈式反序补偿。"""
        if not self._history:
            err = OrderError.nothing_to_undo()
            self._log(f"UNDO -> FAILED: {err}")
            raise err
        command = self._history[-1]
        order_id = command.order_id()
        description = command.describe()
        try:
            order = self._order_or_raise(order_id)
            command.undo(order)
        except OrderError as err:
            self._log(f"UNDO {description} -> FAILED: {err}")
            raise
        # 快照恢复已创建新时间线：同订单更早的命令快照全部失效。
        self._history = [other for other in self._history if other.order_id() != order_id]
        self._log(f"UNDO {description} -> OK")

    def restore(self, order_id: int, memento: OrderMemento) -> None:
        """故障恢复：把订单整单回滚到快照时刻，与命令回滚共用 Memento 机制。
        走审计留痕，不暴露可变引用绕过状态机。"""
        try:
            order = self._order_or_raise(order_id)
            order._restore(memento)
        except OrderError as err:
            self._log(f"RESTORE order={order_id} -> FAILED: {err}")
            raise
        self._history = [command for command in self._history if command.order_id() != order_id]
        self._log(f"RESTORE order={order_id} from [{memento.summary()}] -> OK")

    def order(self, order_id: int) -> Order | None:
        order = self._orders.get(order_id)
        return None if order is None else copy(order)

    @property
    def history_len(self) -> int:
        """已执行未撤销的命令数（对账用）。"""
        return len(self._history)

    @property
    def audit_log(self) -> tuple[str, ...]:
        """审计日志：seq 单调递增，可用于追踪但不能做事件溯源重放（只读视图）。"""
        return tuple(self._audit_log)

    def _order_or_raise(self, order_id: int) -> Order:
        order = self._orders.get(order_id)
        if order is None:
            raise OrderError.order_not_found(order_id)
        return order

    def _log(self, event: str) -> None:
        self._sequence += 1
        self._audit_log.append(f"seq={self._sequence} {event}")
