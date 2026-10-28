package com.xinchentechnote.designpatterns.order_lifecycle;

/**
 * 状态机与命令的失败原因：非法转换必须显式拒绝，绝不静默吞掉。
 * 受检异常迫使调用方逐点处理，与"显式拒绝"的金融工程要求一致。
 */
public final class OrderException extends Exception {

    public enum Reason {
        /** 当前状态不允许该操作（如 Filled 状态撤单） */
        ILLEGAL_TRANSITION,
        /** 成交回报超过剩余量（或为 0） */
        OVERFILL,
        /** 改单数量不得小于等于已成交量 */
        MODIFY_BELOW_FILLED,
        /** 订单不存在 */
        ORDER_NOT_FOUND,
        /** 命令历史为空，无可撤销 */
        NOTHING_TO_UNDO,
        /** 命令执行后订单又发生了变化，旧快照不能覆盖新事件 */
        UNDO_CONFLICT,
        /** 快照属于另一张订单 */
        SNAPSHOT_ORDER_MISMATCH
    }

    private final Reason reason;
    private final String action;
    private final OrderState from;
    private final long requested;
    private final long remaining;
    private final long newQuantity;
    private final long filledQuantity;
    private final long orderId;
    private final long expectedVersion;
    private final long actualVersion;
    private final long snapshotOrderId;

    private OrderException(Reason reason, String message, String action, OrderState from,
                           long requested, long remaining, long newQuantity, long filledQuantity,
                           long orderId, long expectedVersion, long actualVersion, long snapshotOrderId) {
        super(message);
        this.reason = reason;
        this.action = action;
        this.from = from;
        this.requested = requested;
        this.remaining = remaining;
        this.newQuantity = newQuantity;
        this.filledQuantity = filledQuantity;
        this.orderId = orderId;
        this.expectedVersion = expectedVersion;
        this.actualVersion = actualVersion;
        this.snapshotOrderId = snapshotOrderId;
    }

    public static OrderException illegalTransition(String action, OrderState from) {
        return new OrderException(Reason.ILLEGAL_TRANSITION,
                "illegal transition: cannot " + action + " in state " + from,
                action, from, 0, 0, 0, 0, 0, 0, 0, 0);
    }

    public static OrderException overfill(long requested, long remaining) {
        return new OrderException(Reason.OVERFILL,
                "overfill: requested " + requested + " but remaining " + remaining,
                null, null, requested, remaining, 0, 0, 0, 0, 0, 0);
    }

    public static OrderException modifyBelowFilled(long newQuantity, long filledQuantity) {
        return new OrderException(Reason.MODIFY_BELOW_FILLED,
                "modify below filled: new quantity " + newQuantity + " <= filled " + filledQuantity,
                null, null, 0, 0, newQuantity, filledQuantity, 0, 0, 0, 0);
    }

    public static OrderException orderNotFound(long orderId) {
        return new OrderException(Reason.ORDER_NOT_FOUND, "order not found: " + orderId,
                null, null, 0, 0, 0, 0, orderId, 0, 0, 0);
    }

    public static OrderException nothingToUndo() {
        return new OrderException(Reason.NOTHING_TO_UNDO, "nothing to undo",
                null, null, 0, 0, 0, 0, 0, 0, 0, 0);
    }

    public static OrderException undoConflict(long orderId, long expectedVersion, long actualVersion) {
        return new OrderException(Reason.UNDO_CONFLICT,
                "undo conflict: order " + orderId + " expected version " + expectedVersion
                        + " but was " + actualVersion,
                null, null, 0, 0, 0, 0, orderId, expectedVersion, actualVersion, 0);
    }

    public static OrderException snapshotOrderMismatch(long expectedOrderId, long snapshotOrderId) {
        return new OrderException(Reason.SNAPSHOT_ORDER_MISMATCH,
                "snapshot order mismatch: expected order " + expectedOrderId
                        + " but snapshot belongs to " + snapshotOrderId,
                null, null, 0, 0, 0, 0, expectedOrderId, 0, 0, snapshotOrderId);
    }

    public Reason getReason() {
        return reason;
    }

    /** ILLEGAL_TRANSITION：被拒绝的操作名 */
    public String getAction() {
        return action;
    }

    /** ILLEGAL_TRANSITION：转换发生时的状态 */
    public OrderState getFrom() {
        return from;
    }

    /** OVERFILL：回报的成交量 */
    public long getRequested() {
        return requested;
    }

    /** OVERFILL：回报时的剩余量 */
    public long getRemaining() {
        return remaining;
    }

    /** MODIFY_BELOW_FILLED：改单后的数量 */
    public long getNewQuantity() {
        return newQuantity;
    }

    /** MODIFY_BELOW_FILLED：已成交量 */
    public long getFilledQuantity() {
        return filledQuantity;
    }

    /** ORDER_NOT_FOUND：订单 id */
    public long getOrderId() {
        return orderId;
    }

    public long getExpectedVersion() { return expectedVersion; }

    public long getActualVersion() { return actualVersion; }

    public long getSnapshotOrderId() { return snapshotOrderId; }
}
