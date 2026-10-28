package com.xinchentechnote.designpatterns.order_lifecycle;

/**
 * 订单状态（State 模式）：生命周期为
 * Created → Submitted → PartiallyFilled → Filled / Cancelled / Rejected。
 * "当前状态允许哪些操作"集中在枚举方法里，替代散落各处的 {@code if (status == XXX)}。
 */
public enum OrderState {
    /** 已创建，尚未提交 */
    CREATED,
    /** 已提交，等待撮合 */
    SUBMITTED,
    /** 部分成交 */
    PARTIALLY_FILLED,
    /** 全部成交（终态） */
    FILLED,
    /** 已撤销（终态） */
    CANCELLED,
    /** 风控/交易所拒单（终态） */
    REJECTED;

    public boolean isTerminal() {
        return this == FILLED || this == CANCELLED || this == REJECTED;
    }

    public boolean canSubmit() {
        return this == CREATED;
    }

    public boolean canReject() {
        return this == CREATED || this == SUBMITTED;
    }

    /** 只有在途订单（已提交/部分成交）可撤：已成交不可撤是显式规则，不是约定 */
    public boolean canCancel() {
        return this == SUBMITTED || this == PARTIALLY_FILLED;
    }

    /** 在途订单可改单 */
    public boolean canModify() {
        return canCancel();
    }

    /** 在途订单可接受成交回报 */
    public boolean canFill() {
        return canCancel();
    }
}
