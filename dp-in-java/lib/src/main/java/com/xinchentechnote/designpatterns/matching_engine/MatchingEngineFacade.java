package com.xinchentechnote.designpatterns.matching_engine;

/**
 * 撮合引擎门面：对外只暴露 buy / sell / cancel / lastTradePrice 极简接口，
 * 内部装配引擎与默认订阅者（OMS 回报、行情、审计），屏蔽撮合内部复杂度。
 */
public final class MatchingEngineFacade {
    private final MatchingEngine engine = new MatchingEngine();
    private final OmsTradeListener oms = new OmsTradeListener();
    private final MarketDataTradeListener marketData = new MarketDataTradeListener();
    private final AuditTradeListener audit = new AuditTradeListener();

    public MatchingEngineFacade() {
        engine.addTradeListener(oms);
        engine.addTradeListener(marketData);
        engine.addTradeListener(audit);
    }

    public long buy(long price, long quantity) {
        return engine.submit(Side.BUY, price, quantity);
    }

    public long sell(long price, long quantity) {
        return engine.submit(Side.SELL, price, quantity);
    }

    public boolean cancel(long orderId) {
        return engine.cancel(orderId);
    }

    public Long lastTradePrice() {
        return marketData.getLastPrice();
    }
}
