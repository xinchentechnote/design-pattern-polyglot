package com.xinchentechnote.designpatterns.order_lifecycle;

/**
 * 订单快照（Memento）：只含数据不含行为，字段私有。
 * 外部（caretaker）只负责存取，既改不了内容，也读不出细节——
 * {@link #summary()} 仅供审计/对账展示。
 */
public final class OrderMemento {
    // 包私有：窄接口——外部（caretaker）只能 summary() 展示；
    // 宽接口留给同包的原发器 Order 做恢复，快照内容无法被包外改写
    final long orderId;
    final OrderState state;
    final long price;
    final long quantity;
    final long filledQuantity;
    final long version;

    /** 只由原发器 Order 创建，保证快照来源可信 */
    OrderMemento(long orderId, OrderState state, long price, long quantity,
                 long filledQuantity, long version) {
        this.orderId = orderId;
        this.state = state;
        this.price = price;
        this.quantity = quantity;
        this.filledQuantity = filledQuantity;
        this.version = version;
    }

    public long getOrderId() {
        return orderId;
    }

    /** 对账展示用摘要 */
    public String summary() {
        return "order=" + orderId + " state=" + state + " price=" + price + " qty=" + quantity
                + " filled=" + filledQuantity + " v" + version;
    }
}
