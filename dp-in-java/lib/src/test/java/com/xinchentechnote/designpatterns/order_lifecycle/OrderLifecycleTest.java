package com.xinchentechnote.designpatterns.order_lifecycle;

import org.junit.Test;

import java.util.List;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/** 与 Rust 版 order_lifecycle.rs 的 12 例测试断言一一对应。 */
public class OrderLifecycleTest {

    @Test
    public void testSubmitTransitionsCreatedToSubmitted() throws Exception {
        Order order = new Order(1, Side.BUY, 100, 10);
        assertEquals(OrderState.CREATED, order.getState());
        order.submit();
        assertEquals(OrderState.SUBMITTED, order.getState());
        // 已提交的单重复提交：非法转换显式拒绝
        OrderException err = assertThrows(() -> order.submit());
        assertEquals(OrderException.Reason.ILLEGAL_TRANSITION, err.getReason());
        assertEquals("submit", err.getAction());
        assertEquals(OrderState.SUBMITTED, err.getFrom());
    }

    @Test
    public void testRejectFromCreatedState() throws Exception {
        Order order = new Order(1, Side.BUY, 100, 10);
        order.reject();
        assertEquals(OrderState.REJECTED, order.getState());
        assertTrue(order.getState().isTerminal());
        // 终态不可再撤/改/成交
        assertFalse(order.getState().canCancel());
        assertFalse(order.getState().canModify());
        assertFalse(order.getState().canFill());
    }

    @Test
    public void testFillProgressesToPartiallyFilledThenFilled() throws Exception {
        Order order = new Order(1, Side.SELL, 101, 10);
        order.submit();
        order.fill(3);
        assertEquals(OrderState.PARTIALLY_FILLED, order.getState());
        assertEquals(7, order.remainingQuantity());
        order.fill(7);
        assertEquals(OrderState.FILLED, order.getState());
        // 超量成交与终态后成交都显式拒绝
        OrderException terminal = assertThrows(() -> order.fill(1));
        assertEquals(OrderException.Reason.ILLEGAL_TRANSITION, terminal.getReason());
        assertEquals(OrderState.FILLED, terminal.getFrom());

        Order order2 = new Order(2, Side.SELL, 101, 5);
        order2.submit();
        OrderException overfill = assertThrows(() -> order2.fill(6));
        assertEquals(OrderException.Reason.OVERFILL, overfill.getReason());
        assertEquals(6, overfill.getRequested());
        assertEquals(5, overfill.getRemaining());
    }

    @Test
    public void testCancelFilledOrderRejected() throws Exception {
        Oms oms = new Oms();
        long id = oms.create(Side.BUY, 100, 10);
        oms.submit(id);
        oms.fill(id, 10);

        // 已成交不可撤：显式拒绝，订单保持 FILLED
        OrderException err = assertThrows(() -> oms.execute(new CancelCommand(id)));
        assertEquals(OrderException.Reason.ILLEGAL_TRANSITION, err.getReason());
        assertEquals("cancel", err.getAction());
        assertEquals(OrderState.FILLED, err.getFrom());
        assertEquals(OrderState.FILLED, oms.order(id).getState());
        // 失败的命令不进入历史
        assertEquals(0, oms.historyLen());
        assertEquals(OrderException.Reason.NOTHING_TO_UNDO,
                assertThrows(oms::undoLast).getReason());
        assertTrue(oms.auditLog().get(oms.auditLog().size() - 1)
                .contains("UNDO -> FAILED: nothing to undo"));
    }

    @Test
    public void testCancelCommandMovesToCancelled() throws Exception {
        Oms oms = new Oms();
        long id = oms.create(Side.BUY, 100, 10);
        oms.submit(id);
        Order view = oms.order(id);
        view.modify(999, 10);
        assertEquals(100, oms.order(id).getPrice());
        oms.execute(new CancelCommand(id));
        assertEquals(OrderState.CANCELLED, oms.order(id).getState());
        assertEquals(1, oms.historyLen());
    }

    @Test
    public void testUndoCancelRestoresStateAndFills() throws Exception {
        Oms oms = new Oms();
        long id = oms.create(Side.BUY, 100, 10);
        oms.submit(id);
        oms.fill(id, 2); // 部分成交后撤单
        oms.execute(new CancelCommand(id));
        assertEquals(OrderState.CANCELLED, oms.order(id).getState());

        oms.undoLast();
        Order order = oms.order(id);
        // 快照回滚：不只是状态，成交进度一并恢复
        assertEquals(OrderState.PARTIALLY_FILLED, order.getState());
        assertEquals(2, order.getFilledQuantity());
        assertEquals(8, order.remainingQuantity());
        // 恢复后可继续成交
        oms.fill(id, 8);
        assertEquals(OrderState.FILLED, oms.order(id).getState());

        // 历史已空：重复 undo 显式报错（幂等防护）
        assertEquals(OrderException.Reason.NOTHING_TO_UNDO,
                assertThrows(oms::undoLast).getReason());
    }

    @Test
    public void testUndoModifyRestoresPreviousPriceAndQuantity() throws Exception {
        Oms oms = new Oms();
        long id = oms.create(Side.BUY, 100, 10);
        oms.submit(id);
        oms.execute(new ModifyCommand(id, 101, 8));
        oms.execute(new ModifyCommand(id, 102, 9));
        assertEquals(102, oms.order(id).getPrice());
        assertEquals(9, oms.order(id).getQuantity());

        oms.undoLast();
        Order order = oms.order(id);
        assertEquals(101, order.getPrice());
        assertEquals(8, order.getQuantity());
        assertEquals(OrderState.SUBMITTED, order.getState());
        assertEquals(0, oms.historyLen());
        assertEquals(OrderException.Reason.NOTHING_TO_UNDO,
                assertThrows(oms::undoLast).getReason());
    }

    @Test
    public void testModifyQuantityBelowFilledRejected() throws Exception {
        Oms oms = new Oms();
        long id = oms.create(Side.BUY, 100, 10);
        oms.submit(id);
        oms.fill(id, 5);

        // 改单数量必须大于已成交量，否则剩余量为负
        OrderException below = assertThrows(() -> oms.execute(new ModifyCommand(id, 100, 4)));
        assertEquals(OrderException.Reason.MODIFY_BELOW_FILLED, below.getReason());
        assertEquals(4, below.getNewQuantity());
        assertEquals(5, below.getFilledQuantity());
        assertEquals(OrderException.Reason.MODIFY_BELOW_FILLED,
                assertThrows(() -> oms.execute(new ModifyCommand(id, 100, 5))).getReason());

        oms.execute(new ModifyCommand(id, 102, 6));
        assertEquals(102, oms.order(id).getPrice());
        assertEquals(1, oms.order(id).remainingQuantity()); // 6 - 5
    }

    @Test
    public void testAuditTrailRecordsExecuteUndoAndFailures() throws Exception {
        Oms oms = new Oms();
        long id = oms.create(Side.BUY, 100, 10);                                // seq=1
        oms.submit(id);                                                          // seq=2
        oms.fill(id, 10);                                                        // seq=3
        assertThrows(() -> oms.execute(new CancelCommand(id)));                  // seq=4 失败也留痕
        long id2 = oms.create(Side.SELL, 101, 5);                                // seq=5
        oms.submit(id2);                                                         // seq=6
        oms.execute(new CancelCommand(id2));                                     // seq=7
        oms.undoLast();                                                          // seq=8

        List<String> log = oms.auditLog();
        assertEquals(8, log.size());
        assertTrue(log.get(0).contains("seq=1 CREATE order=1"));
        assertTrue(log.get(3).contains("seq=4 EXEC CANCEL order=1 -> FAILED"));
        assertTrue(log.get(3).contains("cannot cancel in state FILLED"));
        assertTrue(log.get(6).contains("seq=7 EXEC CANCEL order=2 -> OK"));
        assertTrue(log.get(7).contains("seq=8 UNDO CANCEL order=2 -> OK"));
        // 回放到 seq=8 时订单 2 应为 SUBMITTED（撤单被补偿）
        assertEquals(OrderState.SUBMITTED, oms.order(id2).getState());
    }

    @Test
    public void testMementoSnapshotAndRestoreForRecovery() throws Exception {
        Oms oms = new Oms();
        long id = oms.create(Side.BUY, 100, 10);
        oms.submit(id);
        oms.fill(id, 3);

        // 故障恢复场景：拍下当前快照后系统继续推进，再整单恢复到快照时刻
        OrderMemento snapshot = oms.order(id).snapshot();
        assertTrue(snapshot.summary().contains("state=PARTIALLY_FILLED"));
        assertTrue(snapshot.summary().contains("filled=3"));

        oms.fill(id, 4);
        oms.execute(new ModifyCommand(id, 105, 12));
        assertEquals(5, oms.order(id).remainingQuantity()); // 12 - 7

        oms.restore(id, snapshot);
        Order order = oms.order(id);
        assertEquals(OrderState.PARTIALLY_FILLED, order.getState());
        assertEquals(3, order.getFilledQuantity());
        assertEquals(100, order.getPrice());
        assertEquals(10, order.getQuantity());
        assertEquals(6, order.getVersion());
        assertEquals(0, oms.historyLen());
        assertTrue(oms.auditLog().get(oms.auditLog().size() - 1).contains("RESTORE order=" + id));
    }

    @Test
    public void testRejectAfterPartialFillIsRejected() throws Exception {
        Order order = new Order(1, Side.BUY, 100, 10);
        order.submit();
        order.fill(2);

        OrderException err = assertThrows(order::reject);
        assertEquals(OrderException.Reason.ILLEGAL_TRANSITION, err.getReason());
        assertEquals("reject", err.getAction());
        assertEquals(OrderState.PARTIALLY_FILLED, err.getFrom());
        assertEquals(OrderState.PARTIALLY_FILLED, order.getState());
    }

    @Test
    public void testUndoRejectsVersionConflictWithoutLosingFill() throws Exception {
        Oms oms = new Oms();
        long id = oms.create(Side.BUY, 100, 10);
        oms.submit(id);
        oms.execute(new ModifyCommand(id, 101, 12));
        oms.fill(id, 3);

        OrderException err = assertThrows(oms::undoLast);
        assertEquals(OrderException.Reason.UNDO_CONFLICT, err.getReason());
        assertEquals(3, err.getExpectedVersion());
        assertEquals(4, err.getActualVersion());
        Order order = oms.order(id);
        assertEquals(101, order.getPrice());
        assertEquals(12, order.getQuantity());
        assertEquals(3, order.getFilledQuantity());
        assertEquals(OrderState.PARTIALLY_FILLED, order.getState());
        assertEquals(1, oms.historyLen());
    }

    private static OrderException assertThrows(ThrowingRunnable runnable) {
        try {
            runnable.run();
        } catch (OrderException expected) {
            return expected;
        } catch (Exception unexpected) {
            throw new AssertionError("unexpected exception type: " + unexpected, unexpected);
        }
        throw new AssertionError("expected OrderException but nothing was thrown");
    }

    @FunctionalInterface
    private interface ThrowingRunnable {
        void run() throws Exception;
    }
}
