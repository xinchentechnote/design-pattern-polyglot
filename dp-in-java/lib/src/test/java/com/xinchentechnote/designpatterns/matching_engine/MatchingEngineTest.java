package com.xinchentechnote.designpatterns.matching_engine;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class MatchingEngineTest {

    private final MatchingEngine engine = new MatchingEngine();

    @Test
    public void testPricePriority() {
        engine.submit(Side.SELL, 101, 5);  // 高价档
        engine.submit(Side.SELL, 100, 5);  // 低价档：价格优先，应先被吃
        OmsTradeListener oms = new OmsTradeListener();
        engine.addTradeListener(oms);

        engine.submit(Side.BUY, 105, 5);

        assertEquals(1, oms.getExecutions().size());
        assertEquals(100, oms.getExecutions().get(0).getPrice());
        assertEquals(5, oms.getExecutions().get(0).getQuantity());
    }

    @Test
    public void testTimePriority() {
        long first = engine.submit(Side.SELL, 100, 3);   // 同价位先到
        long second = engine.submit(Side.SELL, 100, 4);  // 同价位后到
        OmsTradeListener oms = new OmsTradeListener();
        engine.addTradeListener(oms);

        engine.submit(Side.BUY, 100, 5);

        assertEquals(2, oms.getExecutions().size());
        assertEquals(first, oms.getExecutions().get(0).getMakerOrderId());
        assertEquals(3, oms.getExecutions().get(0).getQuantity());
        assertEquals(second, oms.getExecutions().get(1).getMakerOrderId());
        assertEquals(2, oms.getExecutions().get(1).getQuantity());
    }

    @Test
    public void testPartialFill() {
        long sellId = engine.submit(Side.SELL, 100, 10);
        OmsTradeListener oms = new OmsTradeListener();
        engine.addTradeListener(oms);

        engine.submit(Side.BUY, 100, 7);
        assertEquals(1, oms.getExecutions().size());
        assertEquals(7, oms.getExecutions().get(0).getQuantity());

        engine.submit(Side.BUY, 100, 2);
        assertEquals(2, oms.getExecutions().size());
        assertEquals(2, oms.getExecutions().get(1).getQuantity());

        // 吃掉最后 1 手后卖单全部成交、移出在簿集合，撤单应失败
        engine.submit(Side.BUY, 100, 1);
        assertEquals(3, oms.getExecutions().size());
        assertFalse(engine.cancel(sellId));
    }

    @Test
    public void testNoTradeWhenNoCross() {
        OmsTradeListener oms = new OmsTradeListener();
        engine.addTradeListener(oms);

        engine.submit(Side.BUY, 99, 5);
        engine.submit(Side.SELL, 101, 5);

        assertTrue(oms.getExecutions().isEmpty());
    }

    @Test
    public void testCancelRemovesOrderFromBook() {
        long sellId = engine.submit(Side.SELL, 100, 5);
        assertTrue(engine.cancel(sellId));
        assertFalse(engine.cancel(sellId));  // 重复撤单失败

        OmsTradeListener oms = new OmsTradeListener();
        engine.addTradeListener(oms);
        engine.submit(Side.BUY, 100, 5);
        assertTrue(oms.getExecutions().isEmpty());  // 已撤卖单不再参与撮合
    }

    @Test
    public void testTakerCanSweepMultipleLevels() {
        engine.submit(Side.SELL, 101, 3);
        engine.submit(Side.SELL, 100, 4);
        OmsTradeListener oms = new OmsTradeListener();
        engine.addTradeListener(oms);

        engine.submit(Side.BUY, 101, 6);

        assertEquals(2, oms.getExecutions().size());
        assertEquals(100, oms.getExecutions().get(0).getPrice());  // 先吃低档
        assertEquals(4, oms.getExecutions().get(0).getQuantity());
        assertEquals(101, oms.getExecutions().get(1).getPrice());  // 再吃高档
        assertEquals(2, oms.getExecutions().get(1).getQuantity());
    }

    @Test
    public void testMultipleListenersNotified() {
        OmsTradeListener oms = new OmsTradeListener();
        MarketDataTradeListener marketData = new MarketDataTradeListener();
        AuditTradeListener audit = new AuditTradeListener();
        engine.addTradeListener(oms);
        engine.addTradeListener(marketData);
        engine.addTradeListener(audit);

        engine.submit(Side.SELL, 100, 5);
        engine.submit(Side.BUY, 100, 2);

        assertEquals(1, oms.getExecutions().size());
        assertEquals(Long.valueOf(100), marketData.getLastPrice());
        assertEquals(2, marketData.getTotalVolume());
        assertEquals(1, audit.getLogs().size());
        assertTrue(audit.getLogs().get(0).contains("taker=2 maker=1"));
    }
}
