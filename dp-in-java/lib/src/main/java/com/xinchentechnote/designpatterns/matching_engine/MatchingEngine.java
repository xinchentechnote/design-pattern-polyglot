package com.xinchentechnote.designpatterns.matching_engine;

import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.Deque;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.NavigableMap;
import java.util.TreeMap;

/**
 * 撮合引擎（中介者）：买卖订单互不感知，全部通过引擎完成
 * 价格优先、时间优先的连续竞价撮合；成交后向所有 {@link TradeListener}（观察者）推送事件。
 *
 * <p>非线程安全：生产撮合跑在单线程事件循环里，这是"同输入同输出"确定性的前提。
 */
public final class MatchingEngine {
    // 买盘价格从高到低（价格优先）；同价位用 Deque FIFO（时间优先）
    private final NavigableMap<Long, Deque<Order>> bids = new TreeMap<>(Comparator.reverseOrder());
    private final NavigableMap<Long, Deque<Order>> asks = new TreeMap<>();
    // 在簿订单（含部分成交仍有剩余的），id → order
    private final Map<Long, Order> activeOrders = new HashMap<>();
    private final List<TradeListener> tradeListeners = new ArrayList<>();
    // 单调递增序号：既作订单 id / 时间戳，也作成交事件序号，保证撮合确定性
    private long sequence;

    public void addTradeListener(TradeListener listener) {
        tradeListeners.add(listener);
    }

    /**
     * 提交限价单：先作为 taker 尝试与对手簿撮合，剩余量挂簿；返回订单 id。
     */
    public long submit(Side side, long price, long quantity) {
        long id = ++sequence;
        Order order = new Order(id, side, price, quantity, id);
        activeOrders.put(id, order);
        match(order);
        if (order.getLeavesQuantity() > 0) {
            orderBook(side).computeIfAbsent(price, k -> new ArrayDeque<>()).addLast(order);
        } else {
            activeOrders.remove(id);
        }
        return id;
    }

    /**
     * 撤单：从簿中移除。订单不存在或已全部成交返回 false。
     */
    public boolean cancel(long orderId) {
        Order order = activeOrders.remove(orderId);
        if (order == null) {
            return false;
        }
        NavigableMap<Long, Deque<Order>> book = orderBook(order.getSide());
        Deque<Order> level = book.get(order.getPrice());
        level.remove(order);
        if (level.isEmpty()) {
            book.remove(order.getPrice());
        }
        return true;
    }

    private NavigableMap<Long, Deque<Order>> orderBook(Side side) {
        return side == Side.BUY ? bids : asks;
    }

    private void match(Order taker) {
        NavigableMap<Long, Deque<Order>> opposite = orderBook(taker.getSide().opposite());
        while (taker.getLeavesQuantity() > 0 && !opposite.isEmpty()) {
            long bestPrice = opposite.firstKey();
            if (!crosses(taker, bestPrice)) {
                break;
            }
            Deque<Order> level = opposite.get(bestPrice);
            Order maker = level.peekFirst();
            long fillQuantity = Math.min(taker.getLeavesQuantity(), maker.getLeavesQuantity());
            taker.fill(fillQuantity);
            maker.fill(fillQuantity);
            publish(new Trade(taker.getId(), maker.getId(), taker.getSide(),
                    bestPrice, fillQuantity, ++sequence));
            if (maker.getLeavesQuantity() == 0) {
                level.pollFirst();
                activeOrders.remove(maker.getId());
                if (level.isEmpty()) {
                    opposite.remove(bestPrice);
                }
            }
        }
    }

    private boolean crosses(Order taker, long bestPrice) {
        return taker.getSide() == Side.BUY
                ? bestPrice <= taker.getPrice()
                : bestPrice >= taker.getPrice();
    }

    private void publish(Trade trade) {
        for (TradeListener listener : tradeListeners) {
            listener.onTrade(trade);
        }
    }
}
