#include "behavioral/order_lifecycle.h"

#include <algorithm>
namespace dp {
namespace lifecycle {

// ---- OrderException：静态工厂按原因构造，消息与 Rust/Java 版逐字对齐 ----

OrderException OrderException::illegalTransition(std::string action, OrderState from) {
    return OrderException(Reason::IllegalTransition,
                          "illegal transition: cannot " + action + " in state " + stateName(from),
                          std::move(action), from, 0, 0, 0, 0, 0);
}

OrderException OrderException::overfill(uint64_t requested, uint64_t remaining) {
    return OrderException(Reason::Overfill,
                          "overfill: requested " + std::to_string(requested) +
                              " but remaining " + std::to_string(remaining),
                          "", OrderState::Created, requested, remaining, 0, 0, 0);
}

OrderException OrderException::modifyBelowFilled(uint64_t newQuantity, uint64_t filledQuantity) {
    return OrderException(Reason::ModifyBelowFilled,
                          "modify below filled: new quantity " + std::to_string(newQuantity) +
                              " <= filled " + std::to_string(filledQuantity),
                          "", OrderState::Created, 0, 0, newQuantity, filledQuantity, 0);
}

OrderException OrderException::orderNotFound(uint64_t orderId) {
    return OrderException(Reason::OrderNotFound, "order not found: " + std::to_string(orderId),
                          "", OrderState::Created, 0, 0, 0, 0, orderId);
}

OrderException OrderException::nothingToUndo() {
    return OrderException(Reason::NothingToUndo, "nothing to undo", "", OrderState::Created,
                          0, 0, 0, 0, 0);
}

OrderException OrderException::undoConflict(uint64_t orderId, uint64_t expectedVersion,
                                            uint64_t actualVersion) {
    return OrderException(Reason::UndoConflict,
                          "undo conflict: order " + std::to_string(orderId) +
                              " expected version " + std::to_string(expectedVersion) +
                              " but was " + std::to_string(actualVersion),
                          "", OrderState::Created, 0, 0, 0, 0, orderId, expectedVersion,
                          actualVersion);
}

OrderException OrderException::snapshotOrderMismatch(uint64_t expectedOrderId,
                                                     uint64_t snapshotOrderId) {
    return OrderException(Reason::SnapshotOrderMismatch,
                          "snapshot order mismatch: expected order " +
                              std::to_string(expectedOrderId) + " but snapshot belongs to " +
                              std::to_string(snapshotOrderId),
                          "", OrderState::Created, 0, 0, 0, 0, expectedOrderId, 0, 0,
                          snapshotOrderId);
}

// ---- OrderMemento：字段私有，Order 经 friend 读写 ----

OrderMemento::OrderMemento(uint64_t orderId, OrderState state, uint64_t price, uint64_t quantity,
                           uint64_t filledQuantity, uint64_t version)
    : orderId_(orderId), state_(state), price_(price), quantity_(quantity),
      filledQuantity_(filledQuantity), version_(version) {}

std::string OrderMemento::summary() const {
    return "order=" + std::to_string(orderId_) + " state=" + stateName(state_) +
           " price=" + std::to_string(price_) + " qty=" + std::to_string(quantity_) +
           " filled=" + std::to_string(filledQuantity_) + " v" + std::to_string(version_);
}

// ---- Order：原发器，状态迁移的唯一入口 ----

Order::Order(uint64_t id, Side side, uint64_t price, uint64_t quantity)
    : id_(id), side_(side), price_(price), quantity_(quantity), filledQuantity_(0),
      version_(1), state_(OrderState::Created) {}

void Order::submit() {
    if (!canSubmit(state_)) {
        throw OrderException::illegalTransition("submit", state_);
    }
    state_ = OrderState::Submitted;
    ++version_;
}

void Order::reject() {
    if (!canReject(state_)) {
        throw OrderException::illegalTransition("reject", state_);
    }
    state_ = OrderState::Rejected;
    ++version_;
}

void Order::fill(uint64_t quantity) {
    if (!canFill(state_)) {
        throw OrderException::illegalTransition("fill", state_);
    }
    uint64_t remaining = remainingQuantity();
    if (quantity == 0 || quantity > remaining) {
        throw OrderException::overfill(quantity, remaining);
    }
    filledQuantity_ += quantity;
    state_ = filledQuantity_ == quantity_ ? OrderState::Filled : OrderState::PartiallyFilled;
    ++version_;
}

void Order::cancel() {
    if (!canCancel(state_)) {
        throw OrderException::illegalTransition("cancel", state_);
    }
    state_ = OrderState::Cancelled;
    ++version_;
}

void Order::modify(uint64_t newPrice, uint64_t newQuantity) {
    if (!canModify(state_)) {
        throw OrderException::illegalTransition("modify", state_);
    }
    if (newQuantity <= filledQuantity_) {
        throw OrderException::modifyBelowFilled(newQuantity, filledQuantity_);
    }
    price_ = newPrice;
    quantity_ = newQuantity;
    ++version_;
}

OrderMemento Order::snapshot() const {
    return OrderMemento(id_, state_, price_, quantity_, filledQuantity_, version_);
}

void Order::restore(const OrderMemento &memento) {
    if (id_ != memento.orderId_) {
        throw OrderException::snapshotOrderMismatch(id_, memento.orderId_);
    }
    state_ = memento.state_;
    price_ = memento.price_;
    quantity_ = memento.quantity_;
    filledQuantity_ = memento.filledQuantity_;
    version_ = std::max(version_, memento.version_) + 1;
}

// ---- 命令：execute 前拍快照，undo 前用执行版本保护后续事件 ----

std::string CancelCommand::describe() const {
    return "CANCEL order=" + std::to_string(orderId_);
}

void CancelCommand::execute(Order &order) {
    OrderMemento snapshot = order.snapshot();
    order.cancel();
    memento_ = snapshot;
    executedVersion_ = order.version();
}

void CancelCommand::undo(Order &order) {
    if (memento_) {
        if (order.version() != executedVersion_) {
            throw OrderException::undoConflict(orderId_, executedVersion_, order.version());
        }
        order.restore(*memento_);
        memento_.reset();
        executedVersion_ = 0;
    }
}

std::string ModifyCommand::describe() const {
    return "MODIFY order=" + std::to_string(orderId_) + " to price=" +
           std::to_string(newPrice_) + " qty=" + std::to_string(newQuantity_);
}

void ModifyCommand::execute(Order &order) {
    OrderMemento snapshot = order.snapshot();
    order.modify(newPrice_, newQuantity_);
    memento_ = snapshot;
    executedVersion_ = order.version();
}

void ModifyCommand::undo(Order &order) {
    if (memento_) {
        if (order.version() != executedVersion_) {
            throw OrderException::undoConflict(orderId_, executedVersion_, order.version());
        }
        order.restore(*memento_);
        memento_.reset();
        executedVersion_ = 0;
    }
}

// ---- Oms：Invoker + Caretaker，全程审计 ----

uint64_t Oms::create(Side side, uint64_t price, uint64_t quantity) {
    uint64_t id = ++nextOrderId_;
    orders_.try_emplace(id, id, side, price, quantity);
    const char *sideName = side == Side::Buy ? "BUY" : "SELL";
    log("CREATE order=" + std::to_string(id) + " side=" + sideName +
        " price=" + std::to_string(price) + " qty=" + std::to_string(quantity));
    return id;
}

void Oms::submit(uint64_t orderId) {
    try {
        Order &order = orderOrThrow(orderId);
        order.submit();
        log("SUBMIT order=" + std::to_string(orderId));
    } catch (const OrderException &err) {
        log("SUBMIT order=" + std::to_string(orderId) + " FAILED: " + err.what());
        throw;
    }
}

void Oms::reject(uint64_t orderId) {
    try {
        Order &order = orderOrThrow(orderId);
        order.reject();
        log("REJECT order=" + std::to_string(orderId));
    } catch (const OrderException &err) {
        log("REJECT order=" + std::to_string(orderId) + " FAILED: " + err.what());
        throw;
    }
}

void Oms::fill(uint64_t orderId, uint64_t quantity) {
    try {
        Order &order = orderOrThrow(orderId);
        order.fill(quantity);
        log("FILL order=" + std::to_string(orderId) + " qty=" + std::to_string(quantity));
    } catch (const OrderException &err) {
        log("FILL order=" + std::to_string(orderId) + " qty=" + std::to_string(quantity) +
            " FAILED: " + err.what());
        throw;
    }
}

void Oms::execute(std::unique_ptr<OrderCommand> command) {
    const std::string description = command->describe();
    auto it = orders_.find(command->orderId());
    if (it == orders_.end()) {
        OrderException err = OrderException::orderNotFound(command->orderId());
        log("EXEC " + description + " -> FAILED: " + err.what());
        throw err;
    }
    try {
        command->execute(it->second);
    } catch (const OrderException &err) {
        log("EXEC " + description + " -> FAILED: " + err.what());
        throw;
    }
    log("EXEC " + description + " -> OK");
    history_.push_back(std::move(command));
}

void Oms::undoLast() {
    if (history_.empty()) {
        OrderException err = OrderException::nothingToUndo();
        log("UNDO -> FAILED: " + std::string(err.what()));
        throw err;
    }
    OrderCommand &command = *history_.back();
    const uint64_t orderId = command.orderId();
    const std::string description = command.describe();
    try {
        Order &order = orderOrThrow(orderId);
        command.undo(order);
        // 快照恢复已创建新时间线：同订单更早的命令快照全部失效。
        history_.erase(std::remove_if(history_.begin(), history_.end(),
                                      [orderId](const std::unique_ptr<OrderCommand> &other) {
                                          return other->orderId() == orderId;
                                      }),
                       history_.end());
        log("UNDO " + description + " -> OK");
    } catch (const OrderException &err) {
        log("UNDO " + description + " -> FAILED: " + err.what());
        throw;
    }
}

void Oms::restore(uint64_t orderId, const OrderMemento &memento) {
    try {
        Order &order = orderOrThrow(orderId);
        order.restore(memento);
        history_.erase(std::remove_if(history_.begin(), history_.end(),
                                      [orderId](const std::unique_ptr<OrderCommand> &command) {
                                          return command->orderId() == orderId;
                                      }),
                       history_.end());
        log("RESTORE order=" + std::to_string(orderId) + " from [" + memento.summary() +
            "] -> OK");
    } catch (const OrderException &err) {
        log("RESTORE order=" + std::to_string(orderId) + " -> FAILED: " + err.what());
        throw;
    }
}

const Order *Oms::order(uint64_t orderId) const {
    auto it = orders_.find(orderId);
    return it == orders_.end() ? nullptr : &it->second;
}

Order &Oms::orderOrThrow(uint64_t orderId) {
    auto it = orders_.find(orderId);
    if (it == orders_.end()) {
        throw OrderException::orderNotFound(orderId);
    }
    return it->second;
}

void Oms::log(std::string event) {
    ++sequence_;
    auditLog_.push_back("seq=" + std::to_string(sequence_) + " " + std::move(event));
}
}  // namespace lifecycle
}  // namespace dp
