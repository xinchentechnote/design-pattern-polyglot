"""订单生命周期 OMS 测试：与 Rust/Java/C++/Go 版的 12 例测试断言一一对应。"""

import pytest

from order_lifecycle import (
    CancelCommand,
    ModifyCommand,
    Order,
    OrderError,
    OrderErrorReason,
    OrderState,
    Oms,
    Side,
)


def test_submit_transitions_created_to_submitted():
    order = Order(1, Side.BUY, 100, 10)
    assert order.state is OrderState.CREATED
    order.submit()
    assert order.state is OrderState.SUBMITTED
    # 已提交的单重复提交：非法转换显式拒绝
    with pytest.raises(OrderError) as exc_info:
        order.submit()
    err = exc_info.value
    assert err.reason is OrderErrorReason.ILLEGAL_TRANSITION
    assert err.action == "submit"
    assert err.from_state is OrderState.SUBMITTED


def test_reject_from_created_state():
    order = Order(1, Side.BUY, 100, 10)
    order.reject()
    assert order.state is OrderState.REJECTED
    assert order.state.is_terminal
    # 终态不可再撤/改/成交
    assert not order.state.can_cancel
    assert not order.state.can_modify
    assert not order.state.can_fill


def test_fill_progresses_to_partially_filled_then_filled():
    order = Order(1, Side.SELL, 101, 10)
    order.submit()
    order.fill(3)
    assert order.state is OrderState.PARTIALLY_FILLED
    assert order.remaining_quantity == 7
    order.fill(7)
    assert order.state is OrderState.FILLED
    # 超量成交与终态后成交都显式拒绝
    with pytest.raises(OrderError) as exc_info:
        order.fill(1)
    assert exc_info.value.reason is OrderErrorReason.ILLEGAL_TRANSITION
    assert exc_info.value.from_state is OrderState.FILLED

    order2 = Order(2, Side.SELL, 101, 5)
    order2.submit()
    with pytest.raises(OrderError) as exc_info:
        order2.fill(6)
    err = exc_info.value
    assert err.reason is OrderErrorReason.OVERFILL
    assert err.requested == 6
    assert err.remaining == 5


def test_cancel_filled_order_rejected():
    oms = Oms()
    order_id = oms.create(Side.BUY, 100, 10)
    oms.submit(order_id)
    oms.fill(order_id, 10)

    # 已成交不可撤：显式拒绝，订单保持 FILLED
    with pytest.raises(OrderError) as exc_info:
        oms.execute(CancelCommand(order_id))
    err = exc_info.value
    assert err.reason is OrderErrorReason.ILLEGAL_TRANSITION
    assert err.action == "cancel"
    assert err.from_state is OrderState.FILLED
    assert oms.order(order_id).state is OrderState.FILLED
    # 失败的命令不进入历史
    assert oms.history_len == 0
    with pytest.raises(OrderError) as exc_info:
        oms.undo_last()
    assert exc_info.value.reason is OrderErrorReason.NOTHING_TO_UNDO
    assert "UNDO -> FAILED: nothing to undo" in oms.audit_log[-1]


def test_cancel_command_moves_to_cancelled():
    oms = Oms()
    order_id = oms.create(Side.BUY, 100, 10)
    oms.submit(order_id)
    view = oms.order(order_id)
    view.modify(999, 10)
    assert oms.order(order_id).price == 100
    oms.execute(CancelCommand(order_id))
    assert oms.order(order_id).state is OrderState.CANCELLED
    assert oms.history_len == 1


def test_undo_cancel_restores_state_and_fills():
    oms = Oms()
    order_id = oms.create(Side.BUY, 100, 10)
    oms.submit(order_id)
    oms.fill(order_id, 2)  # 部分成交后撤单
    oms.execute(CancelCommand(order_id))
    assert oms.order(order_id).state is OrderState.CANCELLED

    oms.undo_last()
    order = oms.order(order_id)
    # 快照回滚：不只是状态，成交进度一并恢复
    assert order.state is OrderState.PARTIALLY_FILLED
    assert order.filled_quantity == 2
    assert order.remaining_quantity == 8
    # 恢复后可继续成交
    oms.fill(order_id, 8)
    assert oms.order(order_id).state is OrderState.FILLED

    # 历史已空：重复 undo 显式报错（幂等防护）
    with pytest.raises(OrderError) as exc_info:
        oms.undo_last()
    assert exc_info.value.reason is OrderErrorReason.NOTHING_TO_UNDO


def test_undo_modify_restores_previous_price_and_quantity():
    oms = Oms()
    order_id = oms.create(Side.BUY, 100, 10)
    oms.submit(order_id)
    oms.execute(ModifyCommand(order_id, 101, 8))
    oms.execute(ModifyCommand(order_id, 102, 9))
    assert oms.order(order_id).price == 102
    assert oms.order(order_id).quantity == 9

    oms.undo_last()
    order = oms.order(order_id)
    assert order.price == 101
    assert order.quantity == 8
    assert order.state is OrderState.SUBMITTED
    assert oms.history_len == 0
    with pytest.raises(OrderError) as exc_info:
        oms.undo_last()
    assert exc_info.value.reason is OrderErrorReason.NOTHING_TO_UNDO


def test_modify_quantity_below_filled_rejected():
    oms = Oms()
    order_id = oms.create(Side.BUY, 100, 10)
    oms.submit(order_id)
    oms.fill(order_id, 5)

    # 改单数量必须大于已成交量，否则剩余量为负
    with pytest.raises(OrderError) as exc_info:
        oms.execute(ModifyCommand(order_id, 100, 4))
    err = exc_info.value
    assert err.reason is OrderErrorReason.MODIFY_BELOW_FILLED
    assert err.new_quantity == 4
    assert err.filled_quantity == 5
    with pytest.raises(OrderError) as exc_info:
        oms.execute(ModifyCommand(order_id, 100, 5))
    assert exc_info.value.reason is OrderErrorReason.MODIFY_BELOW_FILLED

    oms.execute(ModifyCommand(order_id, 102, 6))
    assert oms.order(order_id).price == 102
    assert oms.order(order_id).remaining_quantity == 1  # 6 - 5


def test_audit_trail_records_execute_undo_and_failures():
    oms = Oms()
    order_id = oms.create(Side.BUY, 100, 10)  # seq=1
    oms.submit(order_id)  # seq=2
    oms.fill(order_id, 10)  # seq=3
    with pytest.raises(OrderError):
        oms.execute(CancelCommand(order_id))  # seq=4 失败也留痕
    order_id2 = oms.create(Side.SELL, 101, 5)  # seq=5
    oms.submit(order_id2)  # seq=6
    oms.execute(CancelCommand(order_id2))  # seq=7
    oms.undo_last()  # seq=8

    log = oms.audit_log
    assert len(log) == 8
    assert "seq=1 CREATE order=1" in log[0]
    assert "seq=4 EXEC CANCEL order=1 -> FAILED" in log[3]
    assert "cannot cancel in state Filled" in log[3]
    assert "seq=7 EXEC CANCEL order=2 -> OK" in log[6]
    assert "seq=8 UNDO CANCEL order=2 -> OK" in log[7]
    # 回放到 seq=8 时订单 2 应为 SUBMITTED（撤单被补偿）
    assert oms.order(order_id2).state is OrderState.SUBMITTED


def test_memento_snapshot_and_restore_for_recovery():
    oms = Oms()
    order_id = oms.create(Side.BUY, 100, 10)
    oms.submit(order_id)
    oms.fill(order_id, 3)

    # 故障恢复场景：拍下当前快照后系统继续推进，再整单恢复到快照时刻
    snapshot = oms.order(order_id).snapshot()
    assert "state=PartiallyFilled" in snapshot.summary()
    assert "filled=3" in snapshot.summary()

    oms.fill(order_id, 4)
    oms.execute(ModifyCommand(order_id, 105, 12))
    assert oms.order(order_id).remaining_quantity == 5  # 12 - 7

    oms.restore(order_id, snapshot)
    order = oms.order(order_id)
    assert order.state is OrderState.PARTIALLY_FILLED
    assert order.filled_quantity == 3
    assert order.price == 100
    assert order.quantity == 10
    assert order.version == 6
    assert oms.history_len == 0
    assert f"RESTORE order={order_id}" in oms.audit_log[-1]


def test_reject_after_partial_fill_is_rejected():
    order = Order(1, Side.BUY, 100, 10)
    order.submit()
    order.fill(2)
    with pytest.raises(OrderError) as exc_info:
        order.reject()
    err = exc_info.value
    assert err.reason is OrderErrorReason.ILLEGAL_TRANSITION
    assert err.action == "reject"
    assert err.from_state is OrderState.PARTIALLY_FILLED
    assert order.state is OrderState.PARTIALLY_FILLED


def test_undo_rejects_version_conflict_without_losing_fill():
    oms = Oms()
    order_id = oms.create(Side.BUY, 100, 10)
    oms.submit(order_id)
    oms.execute(ModifyCommand(order_id, 101, 12))
    oms.fill(order_id, 3)

    with pytest.raises(OrderError) as exc_info:
        oms.undo_last()
    err = exc_info.value
    assert err.reason is OrderErrorReason.UNDO_CONFLICT
    assert err.expected_version == 3
    assert err.actual_version == 4
    order = oms.order(order_id)
    assert order.price == 101
    assert order.quantity == 12
    assert order.filled_quantity == 3
    assert order.state is OrderState.PARTIALLY_FILLED
    assert oms.history_len == 1
