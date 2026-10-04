#include "behavioral/matching_engine.h"
#include "gtest/gtest.h"

#include <memory>
#include <string>

namespace {

TEST(MatchingEngine, test_price_priority) {
    MatchingEngine engine;
    engine.submit(Side::Sell, 101, 5);  // 高价档
    engine.submit(Side::Sell, 100, 5);  // 低价档：价格优先，应先被吃
    auto oms = std::make_shared<OmsTradeListener>();
    engine.addTradeListener(oms);

    engine.submit(Side::Buy, 105, 5);

    ASSERT_EQ(1u, oms->executions().size());
    EXPECT_EQ(100u, oms->executions()[0].price());
    EXPECT_EQ(5u, oms->executions()[0].quantity());
}

TEST(MatchingEngine, test_time_priority) {
    MatchingEngine engine;
    uint64_t first = engine.submit(Side::Sell, 100, 3);   // 同价位先到
    uint64_t second = engine.submit(Side::Sell, 100, 4);  // 同价位后到
    auto oms = std::make_shared<OmsTradeListener>();
    engine.addTradeListener(oms);

    engine.submit(Side::Buy, 100, 5);

    ASSERT_EQ(2u, oms->executions().size());
    EXPECT_EQ(first, oms->executions()[0].makerOrderId());
    EXPECT_EQ(3u, oms->executions()[0].quantity());
    EXPECT_EQ(second, oms->executions()[1].makerOrderId());
    EXPECT_EQ(2u, oms->executions()[1].quantity());
}

TEST(MatchingEngine, test_partial_fill) {
    MatchingEngine engine;
    uint64_t sellId = engine.submit(Side::Sell, 100, 10);
    auto oms = std::make_shared<OmsTradeListener>();
    engine.addTradeListener(oms);

    engine.submit(Side::Buy, 100, 7);
    engine.submit(Side::Buy, 100, 2);
    engine.submit(Side::Buy, 100, 1);

    ASSERT_EQ(3u, oms->executions().size());
    EXPECT_EQ(7u, oms->executions()[0].quantity());
    EXPECT_EQ(2u, oms->executions()[1].quantity());
    EXPECT_FALSE(engine.cancel(sellId));  // 全部成交出簿后撤单失败
}

TEST(MatchingEngine, test_no_trade_when_no_cross) {
    MatchingEngine engine;
    auto oms = std::make_shared<OmsTradeListener>();
    engine.addTradeListener(oms);

    engine.submit(Side::Buy, 99, 5);
    engine.submit(Side::Sell, 101, 5);

    EXPECT_TRUE(oms->executions().empty());
}

TEST(MatchingEngine, test_cancel_removes_order_from_book) {
    MatchingEngine engine;
    uint64_t sellId = engine.submit(Side::Sell, 100, 5);
    EXPECT_TRUE(engine.cancel(sellId));
    EXPECT_FALSE(engine.cancel(sellId));  // 重复撤单失败

    auto oms = std::make_shared<OmsTradeListener>();
    engine.addTradeListener(oms);
    engine.submit(Side::Buy, 100, 5);
    EXPECT_TRUE(oms->executions().empty());  // 已撤卖单不再参与撮合
}

TEST(MatchingEngine, test_taker_can_sweep_multiple_levels) {
    MatchingEngine engine;
    engine.submit(Side::Sell, 101, 3);
    engine.submit(Side::Sell, 100, 4);
    auto oms = std::make_shared<OmsTradeListener>();
    engine.addTradeListener(oms);

    engine.submit(Side::Buy, 101, 6);

    ASSERT_EQ(2u, oms->executions().size());
    EXPECT_EQ(100u, oms->executions()[0].price());  // 先吃低档
    EXPECT_EQ(4u, oms->executions()[0].quantity());
    EXPECT_EQ(101u, oms->executions()[1].price());  // 再吃高档
    EXPECT_EQ(2u, oms->executions()[1].quantity());
}

TEST(MatchingEngine, test_multiple_listeners_notified) {
    MatchingEngine engine;
    auto oms = std::make_shared<OmsTradeListener>();
    auto marketData = std::make_shared<MarketDataTradeListener>();
    auto audit = std::make_shared<AuditTradeListener>();
    engine.addTradeListener(oms);
    engine.addTradeListener(marketData);
    engine.addTradeListener(audit);

    engine.submit(Side::Sell, 100, 5);
    engine.submit(Side::Buy, 100, 2);

    EXPECT_EQ(1u, oms->executions().size());
    EXPECT_EQ(std::optional<uint64_t>(100), marketData->lastPrice());
    EXPECT_EQ(2u, marketData->totalVolume());
    ASSERT_EQ(1u, audit->logs().size());
    EXPECT_NE(std::string::npos, audit->logs()[0].find("taker=2 maker=1"));
}

TEST(MatchingEngineFacade, test_trade_through_facade) {
    MatchingEngineFacade facade;
    facade.sell(100, 5);
    facade.buy(101, 5);
    EXPECT_EQ(std::optional<uint64_t>(100), facade.lastTradePrice());
}

TEST(MatchingEngineFacade, test_cancel_through_facade) {
    MatchingEngineFacade facade;
    uint64_t sellId = facade.sell(100, 5);
    EXPECT_TRUE(facade.cancel(sellId));

    facade.buy(100, 5);
    EXPECT_FALSE(facade.lastTradePrice().has_value());  // 已撤单，无成交
}

}  // namespace
