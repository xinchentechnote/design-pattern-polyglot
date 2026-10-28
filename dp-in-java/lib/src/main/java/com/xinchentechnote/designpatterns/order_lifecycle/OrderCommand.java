package com.xinchentechnote.designpatterns.order_lifecycle;

/**
 * 订单命令（Command 模式）：把撤单/改单从"按钮回调"对象化为
 * 可排队、可审计、可撤销补偿的操作，统一 execute / undo 接口。
 * 并非所有迁移都值得做成命令：成交回报不可撤销，直接走 Order 状态机。
 */
public interface OrderCommand {
    /** 命令描述（审计日志用） */
    String describe();

    /** 目标订单 id */
    long orderId();

    /** 执行命令。失败时订单保持原状，命令不进入历史。 */
    void execute(Order order) throws OrderException;

    /** 仅当订单仍是命令执行后的版本时恢复，避免覆盖后续事件。 */
    void undo(Order order) throws OrderException;
}
