use std::collections::{BTreeMap, HashMap, VecDeque};
use std::sync::{Arc, Mutex};

/// 买卖方向。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Side {
    Buy,
    Sell,
}

impl Side {
    pub fn opposite(self) -> Side {
        match self {
            Side::Buy => Side::Sell,
            Side::Sell => Side::Buy,
        }
    }
}

/// 限价订单。价格与数量用整数 tick 表示（如分），避免浮点误差。
#[derive(Debug)]
pub struct Order {
    pub id: u64,
    pub side: Side,
    pub price: u64,
    pub quantity: u64,
    pub timestamp: u64,
    pub leaves_quantity: u64,
}

impl Order {
    /// 仅撮合引擎调用：按成交量扣减剩余数量
    pub fn fill(&mut self, quantity: u64) {
        self.leaves_quantity -= quantity;
    }
}

/// 成交回报：主动单（taker，新进订单）吃掉被动单（maker，簿内挂单）。
/// 成交价取被动方价格，符合连续竞价"价格优先、时间优先"的撮合惯例。
#[derive(Debug, Clone)]
pub struct Trade {
    pub taker_order_id: u64,
    pub maker_order_id: u64,
    pub taker_side: Side,
    pub price: u64,
    pub quantity: u64,
    pub sequence: u64,
}

/// 观察者：撮合引擎成交事件的订阅方（OMS 回报、行情、审计……）。
pub trait TradeListener {
    fn on_trade(&mut self, trade: &Trade);
}

/// OMS 成交回报订阅者：按到达顺序收集成交，供订单管理侧更新订单状态。
/// 订阅者移入引擎（所有权移交）后，外部仍可通过 shared() 句柄读取。
pub struct OmsTradeListener {
    executions: Arc<Mutex<Vec<Trade>>>,
}

impl OmsTradeListener {
    pub fn new() -> Self {
        Self {
            executions: Arc::new(Mutex::new(Vec::new())),
        }
    }

    pub fn shared(&self) -> Arc<Mutex<Vec<Trade>>> {
        Arc::clone(&self.executions)
    }
}

impl TradeListener for OmsTradeListener {
    fn on_trade(&mut self, trade: &Trade) {
        self.executions.lock().unwrap().push(trade.clone());
    }
}

/// 行情快照状态：最新成交价与今日成交量。
#[derive(Debug, Default)]
pub struct MarketDataSnapshot {
    pub last_price: Option<u64>,
    pub total_volume: u64,
}

/// 行情订阅者：维护最新成交价与今日成交量，生成行情快照。
pub struct MarketDataTradeListener {
    snapshot: Arc<Mutex<MarketDataSnapshot>>,
}

impl MarketDataTradeListener {
    pub fn new() -> Self {
        Self {
            snapshot: Arc::new(Mutex::new(MarketDataSnapshot::default())),
        }
    }

    pub fn shared(&self) -> Arc<Mutex<MarketDataSnapshot>> {
        Arc::clone(&self.snapshot)
    }
}

impl TradeListener for MarketDataTradeListener {
    fn on_trade(&mut self, trade: &Trade) {
        let mut snapshot = self.snapshot.lock().unwrap();
        snapshot.last_price = Some(trade.price);
        snapshot.total_volume += trade.quantity;
    }
}

/// 审计订阅者：每笔成交留痕，日终对账 / 监管报送用。
pub struct AuditTradeListener {
    logs: Arc<Mutex<Vec<String>>>,
}

impl AuditTradeListener {
    pub fn new() -> Self {
        Self {
            logs: Arc::new(Mutex::new(Vec::new())),
        }
    }

    pub fn shared(&self) -> Arc<Mutex<Vec<String>>> {
        Arc::clone(&self.logs)
    }
}

impl TradeListener for AuditTradeListener {
    fn on_trade(&mut self, trade: &Trade) {
        let side = match trade.taker_side {
            Side::Buy => "BUY",
            Side::Sell => "SELL",
        };
        self.logs.lock().unwrap().push(format!(
            "seq={} taker={} maker={} side={} price={} qty={}",
            trade.sequence, trade.taker_order_id, trade.maker_order_id, side, trade.price, trade.quantity
        ));
    }
}

/// 撮合引擎（中介者）：买卖订单互不感知，全部通过引擎完成
/// 价格优先、时间优先的连续竞价撮合；成交后向所有 `TradeListener`（观察者）推送事件。
///
/// 非线程安全：生产撮合跑在单线程事件循环里，这是"同输入同输出"确定性的前提。
#[derive(Default)]
pub struct MatchingEngine {
    /// 买盘：键为价格，取最优买价用 `next_back()`（最高价优先）
    bids: BTreeMap<u64, VecDeque<Order>>,
    /// 卖盘：键升序，`next()` 即最低卖价（价格优先）；同价位 FIFO（时间优先）
    asks: BTreeMap<u64, VecDeque<Order>>,
    /// 在簿订单索引：id → (side, price)；订单本体只存于簿中，保持单一所有权
    active_orders: HashMap<u64, (Side, u64)>,
    listeners: Vec<Box<dyn TradeListener>>,
    /// 单调递增序号：既作订单 id / 时间戳，也作成交事件序号，保证撮合确定性
    sequence: u64,
}

impl MatchingEngine {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn add_trade_listener(&mut self, listener: Box<dyn TradeListener>) {
        self.listeners.push(listener);
    }

    /// 提交限价单：先作为 taker 尝试与对手簿撮合，剩余量挂簿；返回订单 id。
    pub fn submit(&mut self, side: Side, price: u64, quantity: u64) -> u64 {
        self.sequence += 1;
        let id = self.sequence;
        let mut order = Order {
            id,
            side,
            price,
            quantity,
            timestamp: id,
            leaves_quantity: quantity,
        };
        // 借用检查推动了更好的结构：撮合热路径内只收集成交，返回后再统一分发
        let trades = self.match_taker(&mut order);
        for trade in &trades {
            for listener in &mut self.listeners {
                listener.on_trade(trade);
            }
        }
        if order.leaves_quantity > 0 {
            self.book_mut(side)
                .entry(price)
                .or_default()
                .push_back(order);
            self.active_orders.insert(id, (side, price));
        }
        id
    }

    /// 撤单：从簿中移除。订单不存在或已全部成交返回 false。
    pub fn cancel(&mut self, order_id: u64) -> bool {
        let Some((side, price)) = self.active_orders.remove(&order_id) else {
            return false;
        };
        let book = self.book_mut(side);
        if let Some(level) = book.get_mut(&price) {
            level.retain(|order| order.id != order_id);
            if level.is_empty() {
                book.remove(&price); // 空档必须出簿，否则下次撮合拿到空队列
            }
        }
        true
    }

    fn book_mut(&mut self, side: Side) -> &mut BTreeMap<u64, VecDeque<Order>> {
        match side {
            Side::Buy => &mut self.bids,
            Side::Sell => &mut self.asks,
        }
    }

    fn best_crossing_price(&self, taker: &Order) -> Option<u64> {
        match taker.side {
            // 最低卖价 ≤ 买价才交叉
            Side::Buy => self
                .asks
                .keys()
                .next()
                .copied()
                .filter(|&p| p <= taker.price),
            // 最高买价 ≥ 卖价才交叉
            Side::Sell => self
                .bids
                .keys()
                .next_back()
                .copied()
                .filter(|&p| p >= taker.price),
        }
    }

    fn match_taker(&mut self, taker: &mut Order) -> Vec<Trade> {
        let mut trades = Vec::new();
        while taker.leaves_quantity > 0 {
            let Some(best_price) = self.best_crossing_price(taker) else {
                break;
            };
            let (fill_quantity, maker_id, maker_filled) = {
                let level = self
                    .book_mut(taker.side.opposite())
                    .get_mut(&best_price)
                    .expect("最优档必非空（空档即时出簿）");
                let maker = level.front_mut().expect("档位必非空"); // 同价位先来先成交（FIFO）
                let fill_quantity = taker.leaves_quantity.min(maker.leaves_quantity);
                let maker_id = maker.id;
                taker.fill(fill_quantity);
                maker.fill(fill_quantity);
                (fill_quantity, maker_id, maker.leaves_quantity == 0)
            };
            self.sequence += 1;
            trades.push(Trade {
                taker_order_id: taker.id,
                maker_order_id: maker_id,
                taker_side: taker.side,
                price: best_price, // 成交价取被动方（maker）价格
                quantity: fill_quantity,
                sequence: self.sequence,
            });
            if maker_filled {
                let book = self.book_mut(taker.side.opposite());
                if let Some(level) = book.get_mut(&best_price) {
                    level.pop_front();
                    if level.is_empty() {
                        book.remove(&best_price);
                    }
                }
                self.active_orders.remove(&maker_id);
            }
        }
        trades
    }
}

/// 撮合引擎门面：对外只暴露 buy / sell / cancel / last_trade_price 极简接口，
/// 内部装配引擎与默认订阅者（OMS 回报、行情、审计），屏蔽撮合内部复杂度。
pub struct MatchingEngineFacade {
    engine: MatchingEngine,
    // 与引擎内行情订阅者共享的快照句柄：订阅者所有权移交引擎后仍可查询
    market_data: Arc<Mutex<MarketDataSnapshot>>,
}

impl Default for MatchingEngineFacade {
    fn default() -> Self {
        Self::new()
    }
}

impl MatchingEngineFacade {
    pub fn new() -> Self {
        let mut engine = MatchingEngine::new();
        let market_data_listener = MarketDataTradeListener::new();
        let market_data = market_data_listener.shared();
        engine.add_trade_listener(Box::new(OmsTradeListener::new()));
        engine.add_trade_listener(Box::new(market_data_listener));
        engine.add_trade_listener(Box::new(AuditTradeListener::new()));
        Self {
            engine,
            market_data,
        }
    }

    pub fn buy(&mut self, price: u64, quantity: u64) -> u64 {
        self.engine.submit(Side::Buy, price, quantity)
    }

    pub fn sell(&mut self, price: u64, quantity: u64) -> u64 {
        self.engine.submit(Side::Sell, price, quantity)
    }

    pub fn cancel(&mut self, order_id: u64) -> bool {
        self.engine.cancel(order_id)
    }

    pub fn last_trade_price(&self) -> Option<u64> {
        self.market_data.lock().unwrap().last_price
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_price_priority() {
        let mut engine = MatchingEngine::new();
        engine.submit(Side::Sell, 101, 5); // 高价档
        engine.submit(Side::Sell, 100, 5); // 低价档：价格优先，应先被吃
        let oms = OmsTradeListener::new();
        let executions = oms.shared();
        engine.add_trade_listener(Box::new(oms));

        engine.submit(Side::Buy, 105, 5);

        let executions = executions.lock().unwrap();
        assert_eq!(executions.len(), 1);
        assert_eq!(executions[0].price, 100);
        assert_eq!(executions[0].quantity, 5);
    }

    #[test]
    fn test_time_priority() {
        let mut engine = MatchingEngine::new();
        let first = engine.submit(Side::Sell, 100, 3); // 同价位先到
        let second = engine.submit(Side::Sell, 100, 4); // 同价位后到
        let oms = OmsTradeListener::new();
        let executions = oms.shared();
        engine.add_trade_listener(Box::new(oms));

        engine.submit(Side::Buy, 100, 5);

        let executions = executions.lock().unwrap();
        assert_eq!(executions.len(), 2);
        assert_eq!(executions[0].maker_order_id, first);
        assert_eq!(executions[0].quantity, 3);
        assert_eq!(executions[1].maker_order_id, second);
        assert_eq!(executions[1].quantity, 2);
    }

    #[test]
    fn test_partial_fill() {
        let mut engine = MatchingEngine::new();
        let sell_id = engine.submit(Side::Sell, 100, 10);
        let oms = OmsTradeListener::new();
        let executions = oms.shared();
        engine.add_trade_listener(Box::new(oms));

        engine.submit(Side::Buy, 100, 7);
        engine.submit(Side::Buy, 100, 2);
        engine.submit(Side::Buy, 100, 1);

        let executions = executions.lock().unwrap();
        assert_eq!(executions.len(), 3);
        assert_eq!(executions[0].quantity, 7);
        assert_eq!(executions[1].quantity, 2);
        drop(executions);
        assert!(!engine.cancel(sell_id)); // 全部成交出簿后撤单失败
    }

    #[test]
    fn test_no_trade_when_no_cross() {
        let mut engine = MatchingEngine::new();
        let oms = OmsTradeListener::new();
        let executions = oms.shared();
        engine.add_trade_listener(Box::new(oms));

        engine.submit(Side::Buy, 99, 5);
        engine.submit(Side::Sell, 101, 5);

        assert!(executions.lock().unwrap().is_empty());
    }

    #[test]
    fn test_cancel_removes_order_from_book() {
        let mut engine = MatchingEngine::new();
        let sell_id = engine.submit(Side::Sell, 100, 5);
        assert!(engine.cancel(sell_id));
        assert!(!engine.cancel(sell_id)); // 重复撤单失败

        let oms = OmsTradeListener::new();
        let executions = oms.shared();
        engine.add_trade_listener(Box::new(oms));
        engine.submit(Side::Buy, 100, 5);
        assert!(executions.lock().unwrap().is_empty()); // 已撤卖单不再参与撮合
    }

    #[test]
    fn test_taker_can_sweep_multiple_levels() {
        let mut engine = MatchingEngine::new();
        engine.submit(Side::Sell, 101, 3);
        engine.submit(Side::Sell, 100, 4);
        let oms = OmsTradeListener::new();
        let executions = oms.shared();
        engine.add_trade_listener(Box::new(oms));

        engine.submit(Side::Buy, 101, 6);

        let executions = executions.lock().unwrap();
        assert_eq!(executions.len(), 2);
        assert_eq!(executions[0].price, 100); // 先吃低档
        assert_eq!(executions[0].quantity, 4);
        assert_eq!(executions[1].price, 101); // 再吃高档
        assert_eq!(executions[1].quantity, 2);
    }

    #[test]
    fn test_multiple_listeners_notified() {
        let mut engine = MatchingEngine::new();
        let oms = OmsTradeListener::new();
        let executions = oms.shared();
        let market_data_listener = MarketDataTradeListener::new();
        let market_data = market_data_listener.shared();
        let audit = AuditTradeListener::new();
        let logs = audit.shared();
        engine.add_trade_listener(Box::new(oms));
        engine.add_trade_listener(Box::new(market_data_listener));
        engine.add_trade_listener(Box::new(audit));

        engine.submit(Side::Sell, 100, 5);
        engine.submit(Side::Buy, 100, 2);

        assert_eq!(executions.lock().unwrap().len(), 1);
        let snapshot = market_data.lock().unwrap();
        assert_eq!(snapshot.last_price, Some(100));
        assert_eq!(snapshot.total_volume, 2);
        drop(snapshot);
        let logs = logs.lock().unwrap();
        assert_eq!(logs.len(), 1);
        assert!(logs[0].contains("taker=2 maker=1"));
    }

    #[test]
    fn test_trade_through_facade() {
        let mut facade = MatchingEngineFacade::new();
        facade.sell(100, 5);
        facade.buy(101, 5);
        assert_eq!(facade.last_trade_price(), Some(100));
    }

    #[test]
    fn test_cancel_through_facade() {
        let mut facade = MatchingEngineFacade::new();
        let sell_id = facade.sell(100, 5);
        assert!(facade.cancel(sell_id));

        facade.buy(100, 5);
        assert_eq!(facade.last_trade_price(), None); // 已撤单，无成交
    }
}
