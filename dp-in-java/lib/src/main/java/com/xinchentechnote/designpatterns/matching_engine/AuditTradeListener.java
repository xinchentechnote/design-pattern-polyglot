package com.xinchentechnote.designpatterns.matching_engine;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * 审计订阅者：每笔成交留痕，日终对账 / 监管报送用。
 */
public final class AuditTradeListener implements TradeListener {
    private final List<String> logs = new ArrayList<>();

    @Override
    public void onTrade(Trade trade) {
        logs.add(String.format("seq=%d taker=%d maker=%d side=%s price=%d qty=%d",
                trade.getSequence(), trade.getTakerOrderId(), trade.getMakerOrderId(),
                trade.getTakerSide(), trade.getPrice(), trade.getQuantity()));
    }

    public List<String> getLogs() {
        return Collections.unmodifiableList(logs);
    }
}
