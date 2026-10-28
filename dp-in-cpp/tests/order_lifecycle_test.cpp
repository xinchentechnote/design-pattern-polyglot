#include "behavioral/order_lifecycle.h"
#include "gtest/gtest.h"

#include <functional>
#include <memory>
#include <string>

using namespace dp::lifecycle;

namespace {

// 与 Rust 版 order_lifecycle.rs / Java 版 OrderLifecycleTest 的 12 例测试断言一一对应。
// 捕获 OrderException 并返回，便于继续断言原因字段（对应 Rust 的 Err / Java 的 assertThrows）。
OrderException expectFailure(const std::function<void()> &fn) {
    try {
        fn();
    } catch (const OrderException &err) {
        return err;
    }
    ADD_FAILURE() << "expected OrderException but nothing was thrown";
    return OrderException::nothingToUndo();
}

TEST(OrderLifecycle, test_submit_transitions_created_to_submitted) {
    Order order(1, Side::Buy, 100, 10);
    ASSERT_EQ(OrderState::Created, order.state());
    order.submit();
    EXPECT_EQ(OrderState::Submitted, order.state());
    // 已提交的单重复提交：非法转换显式拒绝
    OrderException err = expectFailure([&order] { order.submit(); });
    EXPECT_EQ(OrderException::Reason::IllegalTransition, err.reason());
    EXPECT_EQ("submit", err.action());
    EXPECT_EQ(OrderState::Submitted, err.from());
}

TEST(OrderLifecycle, test_reject_from_created_state) {
    Order order(1, Side::Buy, 100, 10);
    order.reject();
    ASSERT_EQ(OrderState::Rejected, order.state());
    EXPECT_TRUE(isTerminal(order.state()));
    // 终态不可再撤/改/成交
    EXPECT_FALSE(canCancel(order.state()));
    EXPECT_FALSE(canModify(order.state()));
    EXPECT_FALSE(canFill(order.state()));
}

TEST(OrderLifecycle, test_fill_progresses_to_partially_filled_then_filled) {
    Order order(1, Side::Sell, 101, 10);
    order.submit();
    order.fill(3);
    EXPECT_EQ(OrderState::PartiallyFilled, order.state());
    EXPECT_EQ(7u, order.remainingQuantity());
    order.fill(7);
    EXPECT_EQ(OrderState::Filled, order.state());
    // 超量成交与终态后成交都显式拒绝
    OrderException terminal = expectFailure([&order] { order.fill(1); });
    EXPECT_EQ(OrderException::Reason::IllegalTransition, terminal.reason());
    EXPECT_EQ(OrderState::Filled, terminal.from());

    Order order2(2, Side::Sell, 101, 5);
    order2.submit();
    OrderException overfill = expectFailure([&order2] { order2.fill(6); });
    EXPECT_EQ(OrderException::Reason::Overfill, overfill.reason());
    EXPECT_EQ(6u, overfill.requested());
    EXPECT_EQ(5u, overfill.remaining());
}

TEST(OrderLifecycle, test_cancel_filled_order_rejected) {
    Oms oms;
    uint64_t id = oms.create(Side::Buy, 100, 10);
    oms.submit(id);
    oms.fill(id, 10);

    // 已成交不可撤：显式拒绝，订单保持 Filled
    OrderException err = expectFailure([&oms, id] { oms.execute(std::make_unique<CancelCommand>(id)); });
    EXPECT_EQ(OrderException::Reason::IllegalTransition, err.reason());
    EXPECT_EQ("cancel", err.action());
    EXPECT_EQ(OrderState::Filled, err.from());
    ASSERT_NE(nullptr, oms.order(id));
    EXPECT_EQ(OrderState::Filled, oms.order(id)->state());
    // 失败的命令不进入历史
    EXPECT_EQ(0u, oms.historyLen());
    EXPECT_EQ(OrderException::Reason::NothingToUndo, expectFailure([&oms] { oms.undoLast(); }).reason());
    ASSERT_FALSE(oms.auditLog().empty());
    EXPECT_NE(std::string::npos, oms.auditLog().back().find("UNDO -> FAILED: nothing to undo"));
}

TEST(OrderLifecycle, test_cancel_command_moves_to_cancelled) {
    Oms oms;
    uint64_t id = oms.create(Side::Buy, 100, 10);
    oms.submit(id);
    oms.execute(std::make_unique<CancelCommand>(id));
    ASSERT_NE(nullptr, oms.order(id));
    EXPECT_EQ(OrderState::Cancelled, oms.order(id)->state());
    EXPECT_EQ(1u, oms.historyLen());
}

TEST(OrderLifecycle, test_undo_cancel_restores_state_and_fills) {
    Oms oms;
    uint64_t id = oms.create(Side::Buy, 100, 10);
    oms.submit(id);
    oms.fill(id, 2);  // 部分成交后撤单
    oms.execute(std::make_unique<CancelCommand>(id));
    ASSERT_NE(nullptr, oms.order(id));
    EXPECT_EQ(OrderState::Cancelled, oms.order(id)->state());

    oms.undoLast();
    const Order *order = oms.order(id);
    // 快照回滚：不只是状态，成交进度一并恢复
    ASSERT_NE(nullptr, order);
    EXPECT_EQ(OrderState::PartiallyFilled, order->state());
    EXPECT_EQ(2u, order->filledQuantity());
    EXPECT_EQ(8u, order->remainingQuantity());
    // 恢复后可继续成交
    oms.fill(id, 8);
    EXPECT_EQ(OrderState::Filled, oms.order(id)->state());

    // 历史已空：重复 undo 显式报错（幂等防护）
    EXPECT_EQ(OrderException::Reason::NothingToUndo, expectFailure([&oms] { oms.undoLast(); }).reason());
}

TEST(OrderLifecycle, test_undo_modify_restores_previous_price_and_quantity) {
    Oms oms;
    uint64_t id = oms.create(Side::Buy, 100, 10);
    oms.submit(id);
    oms.execute(std::make_unique<ModifyCommand>(id, 101, 8));
    oms.execute(std::make_unique<ModifyCommand>(id, 102, 9));
    ASSERT_NE(nullptr, oms.order(id));
    EXPECT_EQ(102u, oms.order(id)->price());
    EXPECT_EQ(9u, oms.order(id)->quantity());

    oms.undoLast();
    const Order *order = oms.order(id);
    ASSERT_NE(nullptr, order);
    EXPECT_EQ(101u, order->price());
    EXPECT_EQ(8u, order->quantity());
    EXPECT_EQ(OrderState::Submitted, order->state());
    EXPECT_EQ(0u, oms.historyLen());
    EXPECT_EQ(OrderException::Reason::NothingToUndo,
              expectFailure([&oms] { oms.undoLast(); }).reason());
}

TEST(OrderLifecycle, test_modify_quantity_below_filled_rejected) {
    Oms oms;
    uint64_t id = oms.create(Side::Buy, 100, 10);
    oms.submit(id);
    oms.fill(id, 5);

    // 改单数量必须大于已成交量，否则剩余量为负
    OrderException below = expectFailure([&oms, id] { oms.execute(std::make_unique<ModifyCommand>(id, 100, 4)); });
    EXPECT_EQ(OrderException::Reason::ModifyBelowFilled, below.reason());
    EXPECT_EQ(4u, below.newQuantity());
    EXPECT_EQ(5u, below.filledQuantity());
    EXPECT_EQ(OrderException::Reason::ModifyBelowFilled,
              expectFailure([&oms, id] { oms.execute(std::make_unique<ModifyCommand>(id, 100, 5)); }).reason());

    oms.execute(std::make_unique<ModifyCommand>(id, 102, 6));
    ASSERT_NE(nullptr, oms.order(id));
    EXPECT_EQ(102u, oms.order(id)->price());
    EXPECT_EQ(1u, oms.order(id)->remainingQuantity());  // 6 - 5
}

TEST(OrderLifecycle, test_audit_trail_records_execute_undo_and_failures) {
    Oms oms;
    uint64_t id = oms.create(Side::Buy, 100, 10);                                // seq=1
    oms.submit(id);                                                              // seq=2
    oms.fill(id, 10);                                                            // seq=3
    expectFailure([&oms, id] { oms.execute(std::make_unique<CancelCommand>(id)); });  // seq=4 失败也留痕
    uint64_t id2 = oms.create(Side::Sell, 101, 5);                               // seq=5
    oms.submit(id2);                                                             // seq=6
    oms.execute(std::make_unique<CancelCommand>(id2));                           // seq=7
    oms.undoLast();                                                              // seq=8

    const std::vector<std::string> &log = oms.auditLog();
    ASSERT_EQ(8u, log.size());
    EXPECT_NE(std::string::npos, log[0].find("seq=1 CREATE order=1"));
    EXPECT_NE(std::string::npos, log[3].find("seq=4 EXEC CANCEL order=1 -> FAILED"));
    EXPECT_NE(std::string::npos, log[3].find("cannot cancel in state Filled"));
    EXPECT_NE(std::string::npos, log[6].find("seq=7 EXEC CANCEL order=2 -> OK"));
    EXPECT_NE(std::string::npos, log[7].find("seq=8 UNDO CANCEL order=2 -> OK"));
    // 回放到 seq=8 时订单 2 应为 Submitted（撤单被补偿）
    ASSERT_NE(nullptr, oms.order(id2));
    EXPECT_EQ(OrderState::Submitted, oms.order(id2)->state());
}

TEST(OrderLifecycle, test_memento_snapshot_and_restore_for_recovery) {
    Oms oms;
    uint64_t id = oms.create(Side::Buy, 100, 10);
    oms.submit(id);
    oms.fill(id, 3);

    // 故障恢复场景：拍下当前快照后系统继续推进，再整单恢复到快照时刻
    OrderMemento snapshot = oms.order(id)->snapshot();
    EXPECT_NE(std::string::npos, snapshot.summary().find("state=PartiallyFilled"));
    EXPECT_NE(std::string::npos, snapshot.summary().find("filled=3"));

    oms.fill(id, 4);
    oms.execute(std::make_unique<ModifyCommand>(id, 105, 12));
    ASSERT_NE(nullptr, oms.order(id));
    EXPECT_EQ(5u, oms.order(id)->remainingQuantity());  // 12 - 7

    oms.restore(id, snapshot);
    const Order *order = oms.order(id);
    ASSERT_NE(nullptr, order);
    EXPECT_EQ(OrderState::PartiallyFilled, order->state());
    EXPECT_EQ(3u, order->filledQuantity());
    EXPECT_EQ(100u, order->price());
    EXPECT_EQ(10u, order->quantity());
    EXPECT_EQ(6u, order->version());
    EXPECT_EQ(0u, oms.historyLen());
    EXPECT_NE(std::string::npos, oms.auditLog().back().find("RESTORE order=" + std::to_string(id)));
}

TEST(OrderLifecycle, test_reject_after_partial_fill_is_rejected) {
    Order order(1, Side::Buy, 100, 10);
    order.submit();
    order.fill(2);

    OrderException err = expectFailure([&order] { order.reject(); });
    EXPECT_EQ(OrderException::Reason::IllegalTransition, err.reason());
    EXPECT_EQ("reject", err.action());
    EXPECT_EQ(OrderState::PartiallyFilled, err.from());
    EXPECT_EQ(OrderState::PartiallyFilled, order.state());
}

TEST(OrderLifecycle, test_undo_rejects_version_conflict_without_losing_fill) {
    Oms oms;
    uint64_t id = oms.create(Side::Buy, 100, 10);
    oms.submit(id);
    oms.execute(std::make_unique<ModifyCommand>(id, 101, 12));
    oms.fill(id, 3);

    OrderException err = expectFailure([&oms] { oms.undoLast(); });
    EXPECT_EQ(OrderException::Reason::UndoConflict, err.reason());
    EXPECT_EQ(3u, err.expectedVersion());
    EXPECT_EQ(4u, err.actualVersion());
    const Order *order = oms.order(id);
    ASSERT_NE(nullptr, order);
    EXPECT_EQ(101u, order->price());
    EXPECT_EQ(12u, order->quantity());
    EXPECT_EQ(3u, order->filledQuantity());
    EXPECT_EQ(OrderState::PartiallyFilled, order->state());
    EXPECT_EQ(1u, oms.historyLen());
}

}  // namespace
