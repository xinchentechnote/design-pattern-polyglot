"""模拟撮合引擎：中介者 + 观察者 + 门面（实战2 Python 版）。"""

from __future__ import annotations

from abc import ABC, abstractmethod
from collections import deque
from dataclasses import dataclass, field
from enum import Enum


class Side(Enum):
    BUY = "BUY"
    SELL = "SELL"

    def opposite(self) -> Side:
        return Side.SELL if self is Side.BUY else Side.BUY


@dataclass(eq=False)
class Order:
    """限价订单。价格与数量用整数 tick 表示（如分），避免浮点误差。

    eq=False：可变订单用身份相等（同价位去重、撤单移除都按身份），
    避免两张字段恰好相同的订单被误判为同一张。
    """

    id: int
    side: Side
    price: int
    quantity: int
    timestamp: int
    leaves_quantity: int = field(init=False)

    def __post_init__(self) -> None:
        self.leaves_quantity = self.quantity

    def fill(self, quantity: int) -> None:
        """仅撮合引擎调用：按成交量扣减剩余数量。"""
        self.leaves_quantity -= quantity


@dataclass(frozen=True)
class Trade:
    """成交回报：主动单（taker，新进订单）吃掉被动单（maker，簿内挂单）。

    成交价取被动方价格，符合连续竞价"价格优先、时间优先"的撮合惯例。
    不可变值对象：事件发布后任何订阅方都改不动。
    """

    taker_order_id: int
    maker_order_id: int
    taker_side: Side
    price: int
    quantity: int
    sequence: int


class TradeListener(ABC):
    """观察者：撮合引擎成交事件的订阅方（OMS 回报、行情、审计……）。"""

    @abstractmethod
    def on_trade(self, trade: Trade) -> None:
        ...


class OmsTradeListener(TradeListener):
    """OMS 成交回报订阅者：按到达顺序收集成交，供订单管理侧更新订单状态。"""

    def __init__(self) -> None:
        self._executions: list[Trade] = []

    def on_trade(self, trade: Trade) -> None:
        self._executions.append(trade)

    @property
    def executions(self) -> tuple[Trade, ...]:
        return tuple(self._executions)


class MarketDataTradeListener(TradeListener):
    """行情订阅者：维护最新成交价与今日成交量，生成行情快照。"""

    def __init__(self) -> None:
        self._last_price: int | None = None
        self._total_volume = 0

    def on_trade(self, trade: Trade) -> None:
        self._last_price = trade.price
        self._total_volume += trade.quantity

    @property
    def last_price(self) -> int | None:
        return self._last_price

    @property
    def total_volume(self) -> int:
        return self._total_volume


class AuditTradeListener(TradeListener):
    """审计订阅者：每笔成交留痕，日终对账 / 监管报送用。"""

    def __init__(self) -> None:
        self._logs: list[str] = []

    def on_trade(self, trade: Trade) -> None:
        self._logs.append(
            f"seq={trade.sequence} taker={trade.taker_order_id} maker={trade.maker_order_id}"
            f" side={trade.taker_side.value} price={trade.price} qty={trade.quantity}"
        )

    @property
    def logs(self) -> tuple[str, ...]:
        return tuple(self._logs)


class MatchingEngine:
    """撮合引擎（中介者）：买卖订单互不感知，全部通过引擎完成
    价格优先、时间优先的连续竞价撮合；成交后向所有 TradeListener（观察者）推送事件。

    Python 无有序 map，最优档用 min()/max() 现算（教学取舍，O(档位数)；
    生产可用 sortedcontainers.SortedDict 或手写跳表维护有序簿）。
    非线程安全：生产撮合跑在单线程事件循环里，这是"同输入同输出"确定性的前提。
    """

    def __init__(self) -> None:
        self._bids: dict[int, deque[Order]] = {}
        self._asks: dict[int, deque[Order]] = {}
        self._active_orders: dict[int, tuple[Side, int]] = {}  # id → (side, price)，订单本体只存于簿
        self._listeners: list[TradeListener] = []
        self._sequence = 0  # 单调递增序号：既作订单 id / 时间戳，也作成交事件序号，保证撮合确定性

    def add_trade_listener(self, listener: TradeListener) -> None:
        self._listeners.append(listener)

    def submit(self, side: Side, price: int, quantity: int) -> int:
        """提交限价单：先作为 taker 尝试与对手簿撮合，剩余量挂簿；返回订单 id。"""
        self._sequence += 1
        order_id = self._sequence
        order = Order(id=order_id, side=side, price=price, quantity=quantity, timestamp=order_id)
        trades = self._match_taker(order)
        for trade in trades:
            for listener in self._listeners:
                listener.on_trade(trade)
        if order.leaves_quantity > 0:
            self._book(side).setdefault(price, deque()).append(order)
            self._active_orders[order_id] = (side, price)
        return order_id

    def cancel(self, order_id: int) -> bool:
        """撤单：从簿中移除。订单不存在或已全部成交返回 False。"""
        location = self._active_orders.pop(order_id, None)
        if location is None:
            return False
        side, price = location
        level = self._book(side).get(price)
        if level is not None:
            for order in level:
                if order.id == order_id:
                    level.remove(order)  # Order 为身份相等（eq=False），按身份移除
                    break
            if not level:
                del self._book(side)[price]  # 空档必须出簿，否则下次撮合拿到空队列
        return True

    def _book(self, side: Side) -> dict[int, deque[Order]]:
        return self._bids if side is Side.BUY else self._asks

    def _best_crossing_price(self, taker: Order) -> int | None:
        if taker.side is Side.BUY:
            # 最低卖价 ≤ 买价才交叉
            if not self._asks:
                return None
            best = min(self._asks)
            return best if best <= taker.price else None
        # 最高买价 ≥ 卖价才交叉
        if not self._bids:
            return None
        best = max(self._bids)
        return best if best >= taker.price else None

    def _match_taker(self, taker: Order) -> list[Trade]:
        trades: list[Trade] = []
        while taker.leaves_quantity > 0:
            best_price = self._best_crossing_price(taker)
            if best_price is None:
                break
            opposite = self._book(taker.side.opposite())
            level = opposite[best_price]
            maker = level[0]  # 同价位先来先成交（FIFO）
            fill_quantity = min(taker.leaves_quantity, maker.leaves_quantity)
            maker_id = maker.id
            taker.fill(fill_quantity)
            maker.fill(fill_quantity)
            maker_filled = maker.leaves_quantity == 0
            self._sequence += 1
            trades.append(Trade(
                taker_order_id=taker.id,
                maker_order_id=maker_id,
                taker_side=taker.side,
                price=best_price,  # 成交价取被动方（maker）价格
                quantity=fill_quantity,
                sequence=self._sequence,
            ))
            if maker_filled:
                level.popleft()
                if not level:
                    del opposite[best_price]
                del self._active_orders[maker_id]
        return trades


class MatchingEngineFacade:
    """撮合引擎门面：对外只暴露 buy / sell / cancel / last_trade_price 极简接口，
    内部装配引擎与默认订阅者（OMS 回报、行情、审计），屏蔽撮合内部复杂度。"""

    def __init__(self) -> None:
        self._engine = MatchingEngine()
        self._market_data = MarketDataTradeListener()  # 注册进引擎后门面仍持同一对象
        self._engine.add_trade_listener(OmsTradeListener())
        self._engine.add_trade_listener(self._market_data)
        self._engine.add_trade_listener(AuditTradeListener())

    def buy(self, price: int, quantity: int) -> int:
        return self._engine.submit(Side.BUY, price, quantity)

    def sell(self, price: int, quantity: int) -> int:
        return self._engine.submit(Side.SELL, price, quantity)

    def cancel(self, order_id: int) -> bool:
        return self._engine.cancel(order_id)

    @property
    def last_trade_price(self) -> int | None:
        return self._market_data.last_price
