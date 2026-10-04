from matching_engine import (
    AuditTradeListener,
    MarketDataTradeListener,
    MatchingEngine,
    MatchingEngineFacade,
    OmsTradeListener,
    Side,
)


def test_price_priority():
    engine = MatchingEngine()
    engine.submit(Side.SELL, 101, 5)  # 高价档
    engine.submit(Side.SELL, 100, 5)  # 低价档：价格优先，应先被吃
    oms = OmsTradeListener()
    engine.add_trade_listener(oms)

    engine.submit(Side.BUY, 105, 5)

    assert len(oms.executions) == 1
    assert oms.executions[0].price == 100
    assert oms.executions[0].quantity == 5


def test_time_priority():
    engine = MatchingEngine()
    first = engine.submit(Side.SELL, 100, 3)  # 同价位先到
    second = engine.submit(Side.SELL, 100, 4)  # 同价位后到
    oms = OmsTradeListener()
    engine.add_trade_listener(oms)

    engine.submit(Side.BUY, 100, 5)

    assert len(oms.executions) == 2
    assert oms.executions[0].maker_order_id == first
    assert oms.executions[0].quantity == 3
    assert oms.executions[1].maker_order_id == second
    assert oms.executions[1].quantity == 2


def test_partial_fill():
    engine = MatchingEngine()
    sell_id = engine.submit(Side.SELL, 100, 10)
    oms = OmsTradeListener()
    engine.add_trade_listener(oms)

    engine.submit(Side.BUY, 100, 7)
    engine.submit(Side.BUY, 100, 2)
    engine.submit(Side.BUY, 100, 1)

    assert len(oms.executions) == 3
    assert oms.executions[0].quantity == 7
    assert oms.executions[1].quantity == 2
    assert not engine.cancel(sell_id)  # 全部成交出簿后撤单失败


def test_no_trade_when_no_cross():
    engine = MatchingEngine()
    oms = OmsTradeListener()
    engine.add_trade_listener(oms)

    engine.submit(Side.BUY, 99, 5)
    engine.submit(Side.SELL, 101, 5)

    assert len(oms.executions) == 0


def test_cancel_removes_order_from_book():
    engine = MatchingEngine()
    sell_id = engine.submit(Side.SELL, 100, 5)
    assert engine.cancel(sell_id)
    assert not engine.cancel(sell_id)  # 重复撤单失败

    oms = OmsTradeListener()
    engine.add_trade_listener(oms)
    engine.submit(Side.BUY, 100, 5)
    assert len(oms.executions) == 0  # 已撤卖单不再参与撮合


def test_taker_can_sweep_multiple_levels():
    engine = MatchingEngine()
    engine.submit(Side.SELL, 101, 3)
    engine.submit(Side.SELL, 100, 4)
    oms = OmsTradeListener()
    engine.add_trade_listener(oms)

    engine.submit(Side.BUY, 101, 6)

    assert len(oms.executions) == 2
    assert oms.executions[0].price == 100  # 先吃低档
    assert oms.executions[0].quantity == 4
    assert oms.executions[1].price == 101  # 再吃高档
    assert oms.executions[1].quantity == 2


def test_multiple_listeners_notified():
    engine = MatchingEngine()
    oms = OmsTradeListener()
    market_data = MarketDataTradeListener()
    audit = AuditTradeListener()
    engine.add_trade_listener(oms)
    engine.add_trade_listener(market_data)
    engine.add_trade_listener(audit)

    engine.submit(Side.SELL, 100, 5)
    engine.submit(Side.BUY, 100, 2)

    assert len(oms.executions) == 1
    assert market_data.last_price == 100
    assert market_data.total_volume == 2
    assert len(audit.logs) == 1
    assert "taker=2 maker=1" in audit.logs[0]


def test_trade_through_facade():
    facade = MatchingEngineFacade()
    facade.sell(100, 5)
    facade.buy(101, 5)
    assert facade.last_trade_price == 100


def test_cancel_through_facade():
    facade = MatchingEngineFacade()
    sell_id = facade.sell(100, 5)
    assert facade.cancel(sell_id)

    facade.buy(100, 5)
    assert facade.last_trade_price is None  # 已撤单，无成交
