#include "behavioral/matching_engine.h"

#include <algorithm>

namespace {

const char *sideName(Side side) {
    return side == Side::Buy ? "BUY" : "SELL";
}

}  // namespace

Order::Order(uint64_t id, Side side, uint64_t price, uint64_t quantity, uint64_t timestamp)
    : id_(id),
      side_(side),
      price_(price),
      quantity_(quantity),
      timestamp_(timestamp),
      leavesQuantity_(quantity) {}

Trade::Trade(uint64_t takerOrderId, uint64_t makerOrderId, Side takerSide,
             uint64_t price, uint64_t quantity, uint64_t sequence)
    : takerOrderId_(takerOrderId),
      makerOrderId_(makerOrderId),
      takerSide_(takerSide),
      price_(price),
      quantity_(quantity),
      sequence_(sequence) {}

void OmsTradeListener::onTrade(const Trade &trade) {
    executions_.push_back(trade);
}

void MarketDataTradeListener::onTrade(const Trade &trade) {
    lastPrice_ = trade.price();
    totalVolume_ += trade.quantity();
}

void AuditTradeListener::onTrade(const Trade &trade) {
    logs_.push_back("seq=" + std::to_string(trade.sequence()) +
                    " taker=" + std::to_string(trade.takerOrderId()) +
                    " maker=" + std::to_string(trade.makerOrderId()) +
                    " side=" + sideName(trade.takerSide()) +
                    " price=" + std::to_string(trade.price()) +
                    " qty=" + std::to_string(trade.quantity()));
}

void MatchingEngine::addTradeListener(std::shared_ptr<TradeListener> listener) {
    listeners_.push_back(std::move(listener));
}

std::map<uint64_t, std::deque<Order>> &MatchingEngine::book(Side side) {
    return side == Side::Buy ? bids_ : asks_;
}

std::optional<uint64_t> MatchingEngine::bestCrossingPrice(const Order &taker) const {
    if (taker.side() == Side::Buy) {
        // 最低卖价 ≤ 买价才交叉
        if (asks_.empty() || asks_.begin()->first > taker.price()) {
            return std::nullopt;
        }
        return asks_.begin()->first;
    }
    // 最高买价 ≥ 卖价才交叉
    if (bids_.empty() || bids_.rbegin()->first < taker.price()) {
        return std::nullopt;
    }
    return bids_.rbegin()->first;
}

uint64_t MatchingEngine::submit(Side side, uint64_t price, uint64_t quantity) {
    ++sequence_;
    uint64_t id = sequence_;
    Order order(id, side, price, quantity, id);
    // 先在撮合热路径内收集成交，返回后再统一分发
    std::vector<Trade> trades = matchTaker(order);
    for (const Trade &trade : trades) {
        for (auto &listener : listeners_) {
            listener->onTrade(trade);
        }
    }
    if (order.leavesQuantity() > 0) {
        book(side)[price].push_back(std::move(order));
        activeOrders_.emplace(id, std::make_pair(side, price));
    }
    return id;
}

bool MatchingEngine::cancel(uint64_t orderId) {
    auto it = activeOrders_.find(orderId);
    if (it == activeOrders_.end()) {
        return false;
    }
    Side side = it->second.first;
    uint64_t price = it->second.second;
    activeOrders_.erase(it);

    auto &bookRef = book(side);
    auto levelIt = bookRef.find(price);
    if (levelIt != bookRef.end()) {
        auto &level = levelIt->second;
        for (auto orderIt = level.begin(); orderIt != level.end(); ++orderIt) {
            if (orderIt->id() == orderId) {
                level.erase(orderIt);
                break;
            }
        }
        if (level.empty()) {
            bookRef.erase(levelIt);  // 空档必须出簿，否则下次撮合拿到空队列
        }
    }
    return true;
}

std::vector<Trade> MatchingEngine::matchTaker(Order &taker) {
    std::vector<Trade> trades;
    while (taker.leavesQuantity() > 0) {
        std::optional<uint64_t> bestPrice = bestCrossingPrice(taker);
        if (!bestPrice.has_value()) {
            break;
        }
        auto &oppositeBook = book(opposite(taker.side()));
        auto &level = oppositeBook.at(*bestPrice);
        Order &maker = level.front();  // 同价位先来先成交（FIFO）
        uint64_t fillQuantity = std::min(taker.leavesQuantity(), maker.leavesQuantity());
        uint64_t makerId = maker.id();
        taker.fill(fillQuantity);
        maker.fill(fillQuantity);
        bool makerFilled = maker.leavesQuantity() == 0;
        ++sequence_;
        // 成交价取被动方（maker）价格
        trades.emplace_back(taker.id(), makerId, taker.side(), *bestPrice, fillQuantity, sequence_);
        if (makerFilled) {
            level.pop_front();
            if (level.empty()) {
                oppositeBook.erase(*bestPrice);
            }
            activeOrders_.erase(makerId);
        }
    }
    return trades;
}

MatchingEngineFacade::MatchingEngineFacade()
    : engine_(),
      marketData_(std::make_shared<MarketDataTradeListener>()) {
    engine_.addTradeListener(std::make_shared<OmsTradeListener>());
    engine_.addTradeListener(marketData_);
    engine_.addTradeListener(std::make_shared<AuditTradeListener>());
}

uint64_t MatchingEngineFacade::buy(uint64_t price, uint64_t quantity) {
    return engine_.submit(Side::Buy, price, quantity);
}

uint64_t MatchingEngineFacade::sell(uint64_t price, uint64_t quantity) {
    return engine_.submit(Side::Sell, price, quantity);
}

bool MatchingEngineFacade::cancel(uint64_t orderId) {
    return engine_.cancel(orderId);
}

std::optional<uint64_t> MatchingEngineFacade::lastTradePrice() const {
    return marketData_->lastPrice();
}
