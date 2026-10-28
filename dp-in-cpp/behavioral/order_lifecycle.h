#pragma once

// 订单生命周期 OMS（实战3）：状态 + 命令 + 备忘录。
// 与 matching_engine 的 Side 同义但本模块自成一体（跨语言约定一致），
// 两个头文件不会出现在同一个编译单元。

#include <cstdint>
#include <optional>
#include <stdexcept>
#include <string>
#include <unordered_map>
#include <utility>
#include <vector>
namespace dp {
namespace lifecycle {

enum class Side { Buy, Sell };

// 订单状态（State 模式）：生命周期为
// Created → Submitted → PartiallyFilled → Filled / Cancelled / Rejected。
enum class OrderState {
    Created,          // 已创建，尚未提交
    Submitted,        // 已提交，等待撮合
    PartiallyFilled,  // 部分成交
    Filled,           // 全部成交（终态）
    Cancelled,        // 已撤销（终态）
    Rejected,         // 风控/交易所拒单（终态）
};

// "当前状态允许哪些操作"集中在一处，替代散落各处的 if (status == XXX)
inline bool isTerminal(OrderState state) {
    return state == OrderState::Filled || state == OrderState::Cancelled ||
           state == OrderState::Rejected;
}
inline bool canSubmit(OrderState state) { return state == OrderState::Created; }
inline bool canReject(OrderState state) {
    return state == OrderState::Created || state == OrderState::Submitted;
}

// 只有在途订单（已提交/部分成交）可撤：已成交不可撤是显式规则，不是约定
inline bool canCancel(OrderState state) {
    return state == OrderState::Submitted || state == OrderState::PartiallyFilled;
}
inline bool canModify(OrderState state) { return canCancel(state); }
inline bool canFill(OrderState state) { return canCancel(state); }

// 错误消息里的状态名（与 Rust Debug 输出一致，审计日志跨语言可对照）
inline const char *stateName(OrderState state) {
    switch (state) {
    case OrderState::Created: return "Created";
    case OrderState::Submitted: return "Submitted";
    case OrderState::PartiallyFilled: return "PartiallyFilled";
    case OrderState::Filled: return "Filled";
    case OrderState::Cancelled: return "Cancelled";
    case OrderState::Rejected: return "Rejected";
    }
    return "Unknown";
}

// 状态机与命令的失败原因：非法转换必须显式拒绝，绝不静默吞掉。
// C++ 无受检异常，用 reason() 让调用方按原因分派——对应 Rust 的 Result / Java 的受检异常。
class OrderException : public std::runtime_error {
public:
    enum class Reason {
        IllegalTransition,  // 当前状态不允许该操作（如 Filled 状态撤单）
        Overfill,           // 成交回报超过剩余量（或为 0）
        ModifyBelowFilled,  // 改单数量不得小于等于已成交量
        OrderNotFound,      // 订单不存在
        NothingToUndo,      // 命令历史为空，无可撤销
        UndoConflict,       // 命令后又有新事件，禁止旧快照覆盖
        SnapshotOrderMismatch,  // 快照属于另一张订单
    };

    static OrderException illegalTransition(std::string action, OrderState from);
    static OrderException overfill(uint64_t requested, uint64_t remaining);
    static OrderException modifyBelowFilled(uint64_t newQuantity, uint64_t filledQuantity);
    static OrderException orderNotFound(uint64_t orderId);
    static OrderException nothingToUndo();
    static OrderException undoConflict(uint64_t orderId, uint64_t expectedVersion,
                                       uint64_t actualVersion);
    static OrderException snapshotOrderMismatch(uint64_t expectedOrderId,
                                                uint64_t snapshotOrderId);

    Reason reason() const { return reason_; }
    // IllegalTransition：被拒绝的操作名 / 转换发生时的状态
    const std::string &action() const { return action_; }
    OrderState from() const { return from_; }
    // Overfill：回报的成交量 / 回报时的剩余量
    uint64_t requested() const { return requested_; }
    uint64_t remaining() const { return remaining_; }
    // ModifyBelowFilled：改单后的数量 / 已成交量
    uint64_t newQuantity() const { return newQuantity_; }
    uint64_t filledQuantity() const { return filledQuantity_; }
    // OrderNotFound：订单 id
    uint64_t orderId() const { return orderId_; }
    uint64_t expectedVersion() const { return expectedVersion_; }
    uint64_t actualVersion() const { return actualVersion_; }
    uint64_t snapshotOrderId() const { return snapshotOrderId_; }

private:
    OrderException(Reason reason, std::string message, std::string action, OrderState from,
                   uint64_t requested, uint64_t remaining, uint64_t newQuantity,
                   uint64_t filledQuantity, uint64_t orderId, uint64_t expectedVersion = 0,
                   uint64_t actualVersion = 0, uint64_t snapshotOrderId = 0)
        : std::runtime_error(std::move(message)), reason_(reason), action_(std::move(action)),
          from_(from), requested_(requested), remaining_(remaining),
          newQuantity_(newQuantity), filledQuantity_(filledQuantity), orderId_(orderId),
          expectedVersion_(expectedVersion), actualVersion_(actualVersion),
          snapshotOrderId_(snapshotOrderId) {}

    Reason reason_;
    std::string action_;
    OrderState from_ = OrderState::Created;
    uint64_t requested_ = 0;
    uint64_t remaining_ = 0;
    uint64_t newQuantity_ = 0;
    uint64_t filledQuantity_ = 0;
    uint64_t orderId_ = 0;
    uint64_t expectedVersion_ = 0;
    uint64_t actualVersion_ = 0;
    uint64_t snapshotOrderId_ = 0;
};

// 订单快照（Memento）：只含数据不含行为，字段私有。
// C++ 里是纯值类型（全标量字段），命令按值持有——snapshot() 的拷贝代价
// 出现在订单状态含大对象时，可考虑写时复制或增量快照。
// 外部（caretaker）只能 summary() 展示；Order 经 friend 读全量字段做恢复，
// 这是 GoF 宽接口（原发器）/ 窄接口（管理者）在 C++ 的落法。
class OrderMemento {
public:
    // 对账展示用摘要
    std::string summary() const;

private:
    friend class Order;
    OrderMemento(uint64_t orderId, OrderState state, uint64_t price, uint64_t quantity,
                 uint64_t filledQuantity, uint64_t version);

    uint64_t orderId_;
    OrderState state_;
    uint64_t price_;
    uint64_t quantity_;
    uint64_t filledQuantity_;
    uint64_t version_;
};

// 订单（原发器，Originator）：状态的唯一权威持有者。
// state_ 字段私有，外部只能经由 submit / fill / cancel / modify / reject
// 沿状态机迁移；快照与恢复也由订单自己完成——Memento 不破坏封装的关键。
class Order {
public:
    Order(uint64_t id, Side side, uint64_t price, uint64_t quantity);

    uint64_t id() const { return id_; }
    Side side() const { return side_; }
    uint64_t price() const { return price_; }
    uint64_t quantity() const { return quantity_; }
    uint64_t filledQuantity() const { return filledQuantity_; }
    // 数据版本号：每次迁移 +1，快照会记录（乐观锁 / 日终对账用）
    uint64_t version() const { return version_; }
    OrderState state() const { return state_; }
    // 剩余数量 = 委托数量 - 已成交量
    uint64_t remainingQuantity() const { return quantity_ - filledQuantity_; }

    // 以下迁移失败抛 OrderException，订单保持原状
    void submit();    // Created → Submitted
    void reject();    // Created / Submitted → Rejected
    void fill(uint64_t quantity);
    void cancel();
    void modify(uint64_t newPrice, uint64_t newQuantity);

    // 拍快照：命令回滚与故障恢复（日终对账）共用同一机制
    OrderMemento snapshot() const;
private:
    friend class CancelCommand;
    friend class ModifyCommand;
    friend class Oms;
    // 恢复快照中的业务字段；恢复本身是一次新变更，所以版本号只增不减。
    void restore(const OrderMemento &memento);

    uint64_t id_;
    Side side_;
    uint64_t price_;
    uint64_t quantity_;
    uint64_t filledQuantity_;
    uint64_t version_;
    OrderState state_;
};

// 订单命令（Command 模式）：把撤单/改单从"按钮回调"对象化为
// 可排队、可审计、可撤销补偿的操作，统一 execute / undo 接口。
// 并非所有迁移都值得做成命令：成交回报不可撤销，直接走 Order 状态机。
class OrderCommand {
public:
    virtual ~OrderCommand() = default;
    OrderCommand(const OrderCommand &) = delete;
    OrderCommand &operator=(const OrderCommand &) = delete;

    // 命令描述（审计日志用）
    virtual std::string describe() const = 0;
    // 目标订单 id
    virtual uint64_t orderId() const = 0;
    // 执行命令。失败时订单保持原状，命令不进入历史。
    virtual void execute(Order &order) = 0;  // 失败抛 OrderException
    // 仅当订单仍是命令执行后的版本时恢复，避免覆盖后续事件。
    virtual void undo(Order &order) = 0;

protected:
    explicit OrderCommand(uint64_t orderId) : orderId_(orderId) {}

    uint64_t orderId_;
};

// 撤单命令：execute 前先拍快照（Memento），undo 整单恢复——
// 不只是把状态改回去，价格、成交进度一并回到撤单前。
class CancelCommand : public OrderCommand {
public:
    explicit CancelCommand(uint64_t orderId) : OrderCommand(orderId) {}

    std::string describe() const override;
    uint64_t orderId() const override { return orderId_; }
    void execute(Order &order) override;
    void undo(Order &order) override;

private:
    std::optional<OrderMemento> memento_;
    uint64_t executedVersion_ = 0;
};

// 改单命令：记录改单前快照，undo 回滚价格与数量。
// 真实柜台里改单 = 撤旧单 + 下新单（Cancel-Replace），这里简化为原单修订，
// 以演示 Command（对象化操作）与 Memento（快照回滚）的闭环。
class ModifyCommand : public OrderCommand {
public:
    ModifyCommand(uint64_t orderId, uint64_t newPrice, uint64_t newQuantity)
        : OrderCommand(orderId), newPrice_(newPrice), newQuantity_(newQuantity) {}

    std::string describe() const override;
    uint64_t orderId() const override { return orderId_; }
    void execute(Order &order) override;
    void undo(Order &order) override;

private:
    uint64_t newPrice_;
    uint64_t newQuantity_;
    std::optional<OrderMemento> memento_;
    uint64_t executedVersion_ = 0;
};

// OMS（命令调用者 Invoker + 备忘录管理者 Caretaker）：
// 持有全部订单；撤单/改单以命令对象进入，成功后入栈；
// undoLast 只恢复栈顶命令，并清理同订单的旧快照；已进入处理入口的成功与失败均落审计日志，
// 单调递增的序号串成可追踪的时间线，供故障排查与日终对账。
//
// 注意：undo 采用快照整体回滚，演示场景下命令执行与撤销之间
// 不穿插同一订单的成交回报；真实柜台用"补偿命令"（如反向改单）
// 避免覆盖并发成交，快照回滚则留给故障恢复这种"停下来恢复"的场景。
class Oms {
public:
    Oms() = default;
    Oms(const Oms &) = delete;
    Oms &operator=(const Oms &) = delete;

    // 新建订单（Created 状态），返回订单 id
    uint64_t create(Side side, uint64_t price, uint64_t quantity);
    // 提交订单：走状态机，不是命令（提交的补偿就是撤单，无需单独 undo）
    void submit(uint64_t orderId);
    // 拒单回报
    void reject(uint64_t orderId);
    // 成交回报：撮合引擎（实战2）驱动，直接走状态机，不可撤销
    void fill(uint64_t orderId, uint64_t quantity);
    // 执行命令：成功才入栈；失败也留审计痕迹
    void execute(std::unique_ptr<OrderCommand> command);
    // 撤销最近一条命令：成功后清理同订单的其余旧快照
    void undoLast();
    // 故障恢复：把订单整单回滚到快照时刻，与命令回滚共用 Memento 机制。
    // 走审计留痕，不暴露可变引用绕过状态机。
    void restore(uint64_t orderId, const OrderMemento &memento);

    const Order *order(uint64_t orderId) const;
    // 已执行未撤销的命令数（对账用）
    size_t historyLen() const { return history_.size(); }
    // 审计日志：seq 单调递增，可用于追踪；并非事件溯源级可重放日志
    const std::vector<std::string> &auditLog() const { return auditLog_; }

private:
    Order &orderOrThrow(uint64_t orderId);
    void log(std::string event);

    std::unordered_map<uint64_t, Order> orders_;
    // 已执行命令栈：从栈顶 undo；恢复成功会清理同订单的其余旧快照
    std::vector<std::unique_ptr<OrderCommand>> history_;
    std::vector<std::string> auditLog_;
    uint64_t nextOrderId_ = 0;
    // 操作序号：审计流的时间线
    uint64_t sequence_ = 0;
};
}  // namespace lifecycle
}  // namespace dp
