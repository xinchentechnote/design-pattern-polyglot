package com.xinchentechnote.designpatterns.order_lifecycle;

/**
 * 改单命令：记录改单前快照，undo 回滚价格与数量。
 * 真实柜台里改单 = 撤旧单 + 下新单（Cancel-Replace），这里简化为原单修订，
 * 以演示 Command（对象化操作）与 Memento（快照回滚）的闭环。
 */
public final class ModifyCommand implements OrderCommand {
    private final long orderId;
    private final long newPrice;
    private final long newQuantity;
    private OrderMemento memento;
    private long executedVersion;

    public ModifyCommand(long orderId, long newPrice, long newQuantity) {
        this.orderId = orderId;
        this.newPrice = newPrice;
        this.newQuantity = newQuantity;
    }

    @Override
    public String describe() {
        return "MODIFY order=" + orderId + " to price=" + newPrice + " qty=" + newQuantity;
    }

    @Override
    public long orderId() {
        return orderId;
    }

    @Override
    public void execute(Order order) throws OrderException {
        OrderMemento snapshot = order.snapshot();
        order.modify(newPrice, newQuantity);
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
