package com.xinchentechnote.designpatterns.matching_engine;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * OMS 成交回报订阅者：按到达顺序收集成交，供订单管理侧更新订单状态。
 */
public final class OmsTradeListener implements TradeListener {
    private final List<Trade> executions = new ArrayList<>();

    @Override
    public void onTrade(Trade trade) {
        executions.add(trade);
    }

    public List<Trade> getExecutions() {
        return Collections.unmodifiableList(executions);
    }
}
