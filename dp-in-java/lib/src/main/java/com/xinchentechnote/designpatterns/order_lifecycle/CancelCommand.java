package com.xinchentechnote.designpatterns.order_lifecycle;

/**
 * 撤单命令：execute 前先拍快照（Memento），undo 整单恢复——
 * 不只是把状态改回去，价格、成交进度一并回到撤单前。
 */
public final class CancelCommand implements OrderCommand {
    private final long orderId;
    private OrderMemento memento;
    private long executedVersion;

    public CancelCommand(long orderId) {
        this.orderId = orderId;
    }

    @Override
    public String describe() {
        return "CANCEL order=" + orderId;
    }

    @Override
    public long orderId() {
        return orderId;
    }

    @Override
    public void execute(Order order) throws OrderException {
        OrderMemento snapshot = order.snapshot();
        order.cancel();
        memento = snapshot;
        executedVersion = order.getVersion();
    }

    @Override
    public void undo(Order order) throws OrderException {
        if (memento != null) {
            if (order.getVersion() != executedVersion) {
                throw OrderException.undoConflict(orderId, executedVersion, order.getVersion());
            }
            order.restore(memento);
            memento = null;
            executedVersion = 0;
        }
    }
}
