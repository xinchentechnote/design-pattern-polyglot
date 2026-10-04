package com.xinchentechnote.designpatterns.matching_engine;

/**
 * 观察者：撮合引擎成交事件的订阅方（OMS 回报、行情、审计……）。
 */
public interface TradeListener {
    void onTrade(Trade trade);
}
