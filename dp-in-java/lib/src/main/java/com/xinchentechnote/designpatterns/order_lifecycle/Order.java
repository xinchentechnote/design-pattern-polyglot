package com.xinchentechnote.designpatterns.order_lifecycle;

/**
 * 订单（原发器，Originator）：状态的唯一权威持有者。
 * {@code state} 字段私有，外部只能经由 submit / fill / cancel / modify / reject
 * 沿状态机迁移；快照与恢复也由订单自己完成——Memento 不破坏封装的关键。
 */
public final class Order {
    private final long id;
    private final Side side;
    private long price;
    private long quantity;
    private long filledQuantity;
    /** 数据版本号：每次迁移 +1，快照会记录（乐观锁 / 日终对账用） */
    private long version;
    private OrderState state;

    public Order(long id, Side side, long price, long quantity) {
        this.id = id;
        this.side = side;
        this.price = price;
        this.quantity = quantity;
        this.filledQuantity = 0;
        this.version = 1;
        this.state = OrderState.CREATED;
    }

    /** OMS 查询用防御性副本，避免调用方绕过审计修改内部订单。 */
    Order copy() {
        Order copy = new Order(id, side, price, quantity);
        copy.filledQuantity = filledQuantity;
        copy.version = version;
        copy.state = state;
        return copy;
    }

    public long getId() {
        return id;
    }

    public Side getSide() {
        return side;
    }

    public long getPrice() {
        return price;
    }

    public long getQuantity() {
        return quantity;
    }

    public long getFilledQuantity() {
        return filledQuantity;
    }

    public long getVersion() {
        return version;
    }

    public OrderState getState() {
        return state;
    }

    /** 剩余数量 = 委托数量 - 已成交量 */
    public long remainingQuantity() {
        return quantity - filledQuantity;
    }

    /** 提交：Created → Submitted */
    public void submit() throws OrderException {
        if (!state.canSubmit()) {
            throw OrderException.illegalTransition("submit", state);
        }
        state = OrderState.SUBMITTED;
        version++;
    }

    /** 拒单回报：Created / Submitted → Rejected（提交前风控拒，或交易所拒单回报） */
    public void reject() throws OrderException {
        if (!state.canReject()) {
            throw OrderException.illegalTransition("reject", state);
        }
        state = OrderState.REJECTED;
        version++;
    }

    /** 成交回报：按成交量推进状态。剩余量不足显式拒绝。 */
    public void fill(long quantity) throws OrderException {
        if (!state.canFill()) {
            throw OrderException.illegalTransition("fill", state);
        }
        long remaining = remainingQuantity();
        if (quantity <= 0 || quantity > remaining) {
            throw OrderException.overfill(quantity, remaining);
        }
        filledQuantity += quantity;
        state = filledQuantity == this.quantity ? OrderState.FILLED : OrderState.PARTIALLY_FILLED;
        version++;
    }

    /** 撤单：撤销剩余委托量。已成交/已撤销/已拒的单不可撤。 */
    public void cancel() throws OrderException {
        if (!state.canCancel()) {
            throw OrderException.illegalTransition("cancel", state);
        }
        state = OrderState.CANCELLED;
        version++;
    }

    /**
     * 改单（Cancel-Replace 的简化：原单修订价格与数量）。
     * 新数量必须大于已成交量，否则剩余量为负。
     */
    public void modify(long newPrice, long newQuantity) throws OrderException {
        if (!state.canModify()) {
            throw OrderException.illegalTransition("modify", state);
        }
        if (newQuantity <= filledQuantity) {
            throw OrderException.modifyBelowFilled(newQuantity, filledQuantity);
        }
        this.price = newPrice;
        this.quantity = newQuantity;
        version++;
    }

    /** 拍快照：命令回滚与故障恢复（日终对账）共用同一机制 */
    public OrderMemento snapshot() {
        return new OrderMemento(id, state, price, quantity, filledQuantity, version);
    }

    /** 恢复快照中的业务字段；恢复本身是一次新变更，所以版本号只增不减。 */
    void restore(OrderMemento memento) throws OrderException {
        if (id != memento.orderId) {
            throw OrderException.snapshotOrderMismatch(id, memento.orderId);
        }
        this.state = memento.state;
        this.price = memento.price;
        this.quantity = memento.quantity;
        this.filledQuantity = memento.filledQuantity;
        this.version = Math.max(this.version, memento.version) + 1;
    }
}
