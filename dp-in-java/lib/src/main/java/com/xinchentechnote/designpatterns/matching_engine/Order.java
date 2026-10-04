package com.xinchentechnote.designpatterns.matching_engine;

/**
 * 限价订单。价格与数量用整数 tick 表示（如分），避免浮点误差。
 */
public final class Order {
    private final long id;
    private final Side side;
    private final long price;
    private final long quantity;
    private final long timestamp;
    private long leavesQuantity;

    public Order(long id, Side side, long price, long quantity, long timestamp) {
        this.id = id;
        this.side = side;
        this.price = price;
        this.quantity = quantity;
        this.timestamp = timestamp;
        this.leavesQuantity = quantity;
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

    public long getTimestamp() {
        return timestamp;
    }

    public long getLeavesQuantity() {
        return leavesQuantity;
    }

    /** 仅撮合引擎调用：按成交量扣减剩余数量 */
    public void fill(long quantity) {
        this.leavesQuantity -= quantity;
    }
}
