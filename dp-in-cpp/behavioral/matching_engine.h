#pragma once

#include <cstdint>
#include <deque>
#include <map>
#include <memory>
#include <optional>
#include <string>
#include <unordered_map>
#include <utility>
#include <vector>

enum class Side { Buy, Sell };

inline Side opposite(Side side) {
    return side == Side::Buy ? Side::Sell : Side::Buy;
}

// 限价订单。价格与数量用整数 tick 表示（如分），避免浮点误差。
class Order {
public:
    Order(uint64_t id, Side side, uint64_t price, uint64_t quantity, uint64_t timestamp);

    uint64_t id() const { return id_; }
    Side side() const { return side_; }
    uint64_t price() const { return price_; }
    uint64_t quantity() const { return quantity_; }
    uint64_t timestamp() const { return timestamp_; }
    uint64_t leavesQuantity() const { return leavesQuantity_; }

    // 仅撮合引擎调用：按成交量扣减剩余数量
    void fill(uint64_t quantity) { leavesQuantity_ -= quantity; }

private:
    uint64_t id_;
    Side side_;
    uint64_t price_;
    uint64_t quantity_;
    uint64_t timestamp_;
    uint64_t leavesQuantity_;
};

// 成交回报：主动单（taker，新进订单）吃掉被动单（maker，簿内挂单）。
// 成交价取被动方价格，符合连续竞价"价格优先、时间优先"的撮合惯例。
class Trade {
public:
    Trade(uint64_t takerOrderId, uint64_t makerOrderId, Side takerSide,
          uint64_t price, uint64_t quantity, uint64_t sequence);

    uint64_t takerOrderId() const { return takerOrderId_; }
    uint64_t makerOrderId() const { return makerOrderId_; }
    Side takerSide() const { return takerSide_; }
    uint64_t price() const { return price_; }
    uint64_t quantity() const { return quantity_; }
    uint64_t sequence() const { return sequence_; }

private:
    uint64_t takerOrderId_;
    uint64_t makerOrderId_;
    Side takerSide_;
    uint64_t price_;
    uint64_t quantity_;
    uint64_t sequence_;
};

// 观察者：撮合引擎成交事件的订阅方（OMS 回报、行情、审计……）。
class TradeListener {
public:
    virtual ~TradeListener() = default;
    virtual void onTrade(const Trade &trade) = 0;
};

// OMS 成交回报订阅者：按到达顺序收集成交，供订单管理侧更新订单状态。
class OmsTradeListener : public TradeListener {
public:
    void onTrade(const Trade &trade) override;
    const std::vector<Trade> &executions() const { return executions_; }

private:
    std::vector<Trade> executions_;
};

// 行情订阅者：维护最新成交价与今日成交量，生成行情快照。
class MarketDataTradeListener : public TradeListener {
public:
    void onTrade(const Trade &trade) override;
    std::optional<uint64_t> lastPrice() const { return lastPrice_; }
    uint64_t totalVolume() const { return totalVolume_; }

private:
    std::optional<uint64_t> lastPrice_;
    uint64_t totalVolume_ = 0;
};

// 审计订阅者：每笔成交留痕，日终对账 / 监管报送用。
class AuditTradeListener : public TradeListener {
public:
    void onTrade(const Trade &trade) override;
    const std::vector<std::string> &logs() const { return logs_; }

private:
    std::vector<std::string> logs_;
};

// 撮合引擎（中介者）：买卖订单互不感知，全部通过引擎完成
// 价格优先、时间优先的连续竞价撮合；成交后向所有 TradeListener（观察者）推送事件。
// 非线程安全：生产撮合跑在单线程事件循环里，这是"同输入同输出"确定性的前提。
class MatchingEngine {
public:
    MatchingEngine() = default;
    MatchingEngine(const MatchingEngine &) = delete;
    MatchingEngine &operator=(const MatchingEngine &) = delete;

    void addTradeListener(std::shared_ptr<TradeListener> listener);

    // 提交限价单：先作为 taker 尝试与对手簿撮合，剩余量挂簿；返回订单 id。
    uint64_t submit(Side side, uint64_t price, uint64_t quantity);

    // 撤单：从簿中移除。订单不存在或已全部成交返回 false。
    bool cancel(uint64_t orderId);

private:
    std::map<uint64_t, std::deque<Order>> &book(Side side);
    std::optional<uint64_t> bestCrossingPrice(const Order &taker) const;
    std::vector<Trade> matchTaker(Order &taker);

    // 买盘：键升序，最优买价取 rbegin（最高价优先）
    std::map<uint64_t, std::deque<Order>> bids_;
    // 卖盘：键升序，begin 即最低卖价（价格优先）；同价位 deque FIFO（时间优先）
    std::map<uint64_t, std::deque<Order>> asks_;
    // 在簿订单索引：id → (side, price)；订单本体只存于簿中
    std::unordered_map<uint64_t, std::pair<Side, uint64_t>> activeOrders_;
    std::vector<std::shared_ptr<TradeListener>> listeners_;
    // 单调递增序号：既作订单 id / 时间戳，也作成交事件序号，保证撮合确定性
    uint64_t sequence_ = 0;
};

// 撮合引擎门面：对外只暴露 buy / sell / cancel / lastTradePrice 极简接口，
// 内部装配引擎与默认订阅者（OMS 回报、行情、审计），屏蔽撮合内部复杂度。
class MatchingEngineFacade {
public:
    MatchingEngineFacade();

    uint64_t buy(uint64_t price, uint64_t quantity);
    uint64_t sell(uint64_t price, uint64_t quantity);
    bool cancel(uint64_t orderId);
    std::optional<uint64_t> lastTradePrice() const;

private:
    MatchingEngine engine_;
    // 行情订阅者的共享句柄：注册进引擎后门面仍可查询最新价
    std::shared_ptr<MarketDataTradeListener> marketData_;
};
