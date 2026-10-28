package com.xinchentechnote.designpatterns.order_lifecycle;

import java.util.ArrayDeque;
import java.util.Collections;
import java.util.Deque;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * OMS（命令调用者 Invoker + 备忘录管理者 Caretaker）：
 * 持有全部订单；撤单/改单以命令对象进入，成功后入栈；
 * undoLast 只恢复栈顶命令，并清理同订单的旧快照；已进入处理入口的成功与失败均落审计日志，
 * 单调递增的序号串成可追踪的时间线，供故障排查与日终对账。
 *
 * <p>注意：undo 采用快照整体回滚，演示场景下命令执行与撤销之间
 * 不穿插同一订单的成交回报；真实柜台用"补偿命令"（如反向改单）
 * 避免覆盖并发成交，快照回滚则留给故障恢复这种"停下来恢复"的场景。
 */
public final class Oms {
    private final Map<Long, Order> orders = new HashMap<>();
    /** 已执行命令栈：从栈顶 undo；恢复成功会清理同订单的其余旧快照 */
    private final Deque<OrderCommand> history = new ArrayDeque<>();
    private final List<String> auditLog = new ArrayList<>();
    private long nextOrderId;
    /** 操作序号：审计流的时间线 */
    private long sequence;

    /** 新建订单（Created 状态），返回订单 id */
    public long create(Side side, long price, long quantity) {
        long id = ++nextOrderId;
        orders.put(id, new Order(id, side, price, quantity));
        String sideName = side == Side.BUY ? "BUY" : "SELL";
        log("CREATE order=" + id + " side=" + sideName + " price=" + price + " qty=" + quantity);
        return id;
    }

    /** 提交订单：走状态机，不是命令（提交的补偿就是撤单，无需单独 undo） */
    public void submit(long orderId) throws OrderException {
        try {
            Order order = orderOrThrow(orderId);
            order.submit();
            log("SUBMIT order=" + orderId);
        } catch (OrderException err) {
            log("SUBMIT order=" + orderId + " FAILED: " + err.getMessage());
            throw err;
        }
    }

    /** 拒单回报 */
    public void reject(long orderId) throws OrderException {
        try {
            Order order = orderOrThrow(orderId);
            order.reject();
            log("REJECT order=" + orderId);
        } catch (OrderException err) {
            log("REJECT order=" + orderId + " FAILED: " + err.getMessage());
            throw err;
        }
    }

    /** 成交回报：撮合引擎（实战2）驱动，直接走状态机，不可撤销 */
    public void fill(long orderId, long quantity) throws OrderException {
        try {
            Order order = orderOrThrow(orderId);
            order.fill(quantity);
            log("FILL order=" + orderId + " qty=" + quantity);
        } catch (OrderException err) {
            log("FILL order=" + orderId + " qty=" + quantity + " FAILED: " + err.getMessage());
            throw err;
        }
    }

    /** 执行命令：成功才入栈；失败也留审计痕迹 */
    public void execute(OrderCommand command) throws OrderException {
        String description = command.describe();
        Order order = orders.get(command.orderId());
        if (order == null) {
            OrderException err = OrderException.orderNotFound(command.orderId());
            log("EXEC " + description + " -> FAILED: " + err.getMessage());
            throw err;
        }
        try {
            command.execute(order);
        } catch (OrderException err) {
            log("EXEC " + description + " -> FAILED: " + err.getMessage());
            throw err;
        }
        log("EXEC " + description + " -> OK");
        history.push(command);
    }

    /** 撤销最近一条命令：版本冲突时拒绝并保留命令历史 */
    public void undoLast() throws OrderException {
        if (history.isEmpty()) {
            OrderException err = OrderException.nothingToUndo();
            log("UNDO -> FAILED: " + err.getMessage());
            throw err;
        }
        OrderCommand command = history.peek();
        long orderId = command.orderId();
        String description = command.describe();
        try {
            Order order = orderOrThrow(orderId);
            command.undo(order);
            // 快照恢复已创建新时间线：同订单更早的命令快照全部失效。
            history.removeIf(other -> other.orderId() == orderId);
            log("UNDO " + description + " -> OK");
        } catch (OrderException err) {
            log("UNDO " + description + " -> FAILED: " + err.getMessage());
            throw err;
        }
    }

    /**
     * 故障恢复：把订单整单回滚到快照时刻，与命令回滚共用 Memento 机制。
     * 走审计留痕，不暴露可变引用绕过状态机。
     */
    public void restore(long orderId, OrderMemento memento) throws OrderException {
        try {
            Order order = orderOrThrow(orderId);
            order.restore(memento);
            history.removeIf(command -> command.orderId() == orderId);
            log("RESTORE order=" + orderId + " from [" + memento.summary() + "] -> OK");
        } catch (OrderException err) {
            log("RESTORE order=" + orderId + " -> FAILED: " + err.getMessage());
            throw err;
        }
    }

    public Order order(long orderId) {
        Order order = orders.get(orderId);
        return order == null ? null : order.copy();
    }

    /** 已执行未撤销的命令数（对账用） */
    public int historyLen() {
        return history.size();
    }

    /** 审计日志：seq 单调递增，可用于追踪；并非事件溯源级可重放日志 */
    public List<String> auditLog() {
        return Collections.unmodifiableList(auditLog);
    }

    private Order orderOrThrow(long orderId) throws OrderException {
        Order order = orders.get(orderId);
        if (order == null) {
            throw OrderException.orderNotFound(orderId);
        }
        return order;
    }

    private void log(String event) {
        sequence++;
        auditLog.add("seq=" + sequence + " " + event);
    }
}
