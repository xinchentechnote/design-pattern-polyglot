package com.xinchentechnote.designpatterns.matching_engine;

/**
 * 行情订阅者：维护最新成交价与今日成交量，生成行情快照。
 */
public final class MarketDataTradeListener implements TradeListener {
    private Long lastPrice;
    private long totalVolume;

    @Override
    public void onTrade(Trade trade) {
        this.lastPrice = trade.getPrice();
        this.totalVolume += trade.getQuantity();
    }

    public Long getLastPrice() {
        return lastPrice;
    }

    public long getTotalVolume() {
        return totalVolume;
    }
}
