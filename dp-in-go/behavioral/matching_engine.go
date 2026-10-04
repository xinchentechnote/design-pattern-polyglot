package behavioral

import (
	"fmt"
	"sort"
)

// Side 买卖方向。
type Side int

const (
	SideBuy Side = iota + 1
	SideSell
)

func (s Side) Opposite() Side {
	if s == SideBuy {
		return SideSell
	}
	return SideBuy
}

func (s Side) String() string {
	if s == SideBuy {
		return "BUY"
	}
	return "SELL"
}

// Order 限价订单。价格与数量用整数 tick 表示（如分），避免浮点误差。
type Order struct {
	ID             uint64
	Side           Side
	Price          uint64
	Quantity       uint64
	Timestamp      uint64
	LeavesQuantity uint64
}

// Fill 仅撮合引擎调用：按成交量扣减剩余数量。
func (o *Order) Fill(quantity uint64) {
	o.LeavesQuantity -= quantity
}

// Trade 成交回报：主动单（taker，新进订单）吃掉被动单（maker，簿内挂单）。
// 成交价取被动方价格，符合连续竞价"价格优先、时间优先"的撮合惯例。
type Trade struct {
	TakerOrderID uint64
	MakerOrderID uint64
	TakerSide    Side
	Price        uint64
	Quantity     uint64
	Sequence     uint64
}

// TradeListener 观察者：撮合引擎成交事件的订阅方（OMS 回报、行情、审计……）。
type TradeListener interface {
	OnTrade(trade Trade)
}

// OmsTradeListener OMS 成交回报订阅者：按到达顺序收集成交，供订单管理侧更新订单状态。
type OmsTradeListener struct {
	executions []Trade
}

func NewOmsTradeListener() *OmsTradeListener {
	return &OmsTradeListener{}
}

func (l *OmsTradeListener) OnTrade(trade Trade) {
	l.executions = append(l.executions, trade)
}

func (l *OmsTradeListener) Executions() []Trade {
	return l.executions
}

// MarketDataTradeListener 行情订阅者：维护最新成交价与今日成交量，生成行情快照。
type MarketDataTradeListener struct {
	lastPrice   uint64
	hasLast     bool
	totalVolume uint64
}

func NewMarketDataTradeListener() *MarketDataTradeListener {
	return &MarketDataTradeListener{}
}

func (l *MarketDataTradeListener) OnTrade(trade Trade) {
	l.lastPrice = trade.Price
	l.hasLast = true
	l.totalVolume += trade.Quantity
}

// LastPrice 无成交时第二个返回值为 false。
func (l *MarketDataTradeListener) LastPrice() (uint64, bool) {
	return l.lastPrice, l.hasLast
}

func (l *MarketDataTradeListener) TotalVolume() uint64 {
	return l.totalVolume
}

// AuditTradeListener 审计订阅者：每笔成交留痕，日终对账 / 监管报送用。
type AuditTradeListener struct {
	logs []string
}

func NewAuditTradeListener() *AuditTradeListener {
	return &AuditTradeListener{}
}

func (l *AuditTradeListener) OnTrade(trade Trade) {
	l.logs = append(l.logs, fmt.Sprintf("seq=%d taker=%d maker=%d side=%s price=%d qty=%d",
		trade.Sequence, trade.TakerOrderID, trade.MakerOrderID, trade.TakerSide, trade.Price, trade.Quantity))
}

func (l *AuditTradeListener) Logs() []string {
	return l.logs
}

// orderLocation 在簿订单索引：只存位置（边 + 价格档），订单本体只存于簿中。
type orderLocation struct {
	side  Side
	price uint64
}

// MatchingEngine 撮合引擎（中介者）：买卖订单互不感知，全部通过引擎完成
// 价格优先、时间优先的连续竞价撮合；成交后向所有 TradeListener（观察者）推送事件。
//
// Go 没有有序 map，订单簿用"map[价格]FIFO 队列 + 有序价格档切片"自维护：
// 买盘档位降序（bidLevels[0] 即最高买价），卖盘档位升序（askLevels[0] 即最低卖价）。
// 非线程安全：生产撮合跑在单线程事件循环里，这是"同输入同输出"确定性的前提。
type MatchingEngine struct {
	bids          map[uint64][]*Order
	asks          map[uint64][]*Order
	bidLevels     []uint64 // 降序
	askLevels     []uint64 // 升序
	activeOrders  map[uint64]orderLocation
	listeners     []TradeListener
	sequence      uint64 // 单调递增序号：既作订单 id / 时间戳，也作成交事件序号，保证撮合确定性
}

func NewMatchingEngine() *MatchingEngine {
	return &MatchingEngine{
		bids:         make(map[uint64][]*Order),
		asks:         make(map[uint64][]*Order),
		activeOrders: make(map[uint64]orderLocation),
	}
}

func (e *MatchingEngine) AddTradeListener(listener TradeListener) {
	e.listeners = append(e.listeners, listener)
}

// Submit 提交限价单：先作为 taker 尝试与对手簿撮合，剩余量挂簿；返回订单 id。
func (e *MatchingEngine) Submit(side Side, price, quantity uint64) uint64 {
	e.sequence++
	id := e.sequence
	order := &Order{
		ID:             id,
		Side:           side,
		Price:          price,
		Quantity:       quantity,
		Timestamp:      id,
		LeavesQuantity: quantity,
	}
	// 先在撮合热路径内收集成交，返回后再统一分发
	trades := e.matchTaker(order)
	for i := range trades {
		for _, listener := range e.listeners {
			listener.OnTrade(trades[i])
		}
	}
	if order.LeavesQuantity > 0 {
		e.restOrder(order)
	}
	return id
}

// Cancel 撤单：从簿中移除。订单不存在或已全部成交返回 false。
func (e *MatchingEngine) Cancel(orderID uint64) bool {
	loc, ok := e.activeOrders[orderID]
	if !ok {
		return false
	}
	delete(e.activeOrders, orderID)

	book, _ := e.bookSide(loc.side)
	queue := book[loc.price]
	for i, o := range queue {
		if o.ID == orderID {
			queue = append(queue[:i], queue[i+1:]...)
			break
		}
	}
	if len(queue) == 0 {
		delete(book, loc.price)
		e.removeLevel(loc.side, loc.price) // 空档必须出簿，否则下次撮合拿到空队列
	} else {
		book[loc.price] = queue
	}
	return true
}

func (e *MatchingEngine) restOrder(order *Order) {
	book, _ := e.bookSide(order.Side)
	if _, ok := book[order.Price]; !ok {
		e.insertLevel(order.Side, order.Price)
	}
	book[order.Price] = append(book[order.Price], order)
	e.activeOrders[order.ID] = orderLocation{side: order.Side, price: order.Price}
}

func (e *MatchingEngine) bookSide(side Side) (map[uint64][]*Order, *[]uint64) {
	if side == SideBuy {
		return e.bids, &e.bidLevels
	}
	return e.asks, &e.askLevels
}

func (e *MatchingEngine) insertLevel(side Side, price uint64) {
	if side == SideBuy {
		// 买盘档位降序：插到第一个小于 price 的位置前
		i := sort.Search(len(e.bidLevels), func(i int) bool { return e.bidLevels[i] < price })
		e.bidLevels = append(e.bidLevels, 0)
		copy(e.bidLevels[i+1:], e.bidLevels[i:])
		e.bidLevels[i] = price
		return
	}
	// 卖盘档位升序：插到第一个大于 price 的位置前
	i := sort.Search(len(e.askLevels), func(i int) bool { return e.askLevels[i] > price })
	e.askLevels = append(e.askLevels, 0)
	copy(e.askLevels[i+1:], e.askLevels[i:])
	e.askLevels[i] = price
}

func (e *MatchingEngine) removeLevel(side Side, price uint64) {
	if side == SideBuy {
		i := sort.Search(len(e.bidLevels), func(i int) bool { return e.bidLevels[i] <= price })
		if i < len(e.bidLevels) && e.bidLevels[i] == price {
			e.bidLevels = append(e.bidLevels[:i], e.bidLevels[i+1:]...)
		}
		return
	}
	i := sort.Search(len(e.askLevels), func(i int) bool { return e.askLevels[i] >= price })
	if i < len(e.askLevels) && e.askLevels[i] == price {
		e.askLevels = append(e.askLevels[:i], e.askLevels[i+1:]...)
	}
}

func (e *MatchingEngine) bestCrossingPrice(taker *Order) (uint64, bool) {
	if taker.Side == SideBuy {
		// 最低卖价 ≤ 买价才交叉
		if len(e.askLevels) == 0 || e.askLevels[0] > taker.Price {
			return 0, false
		}
		return e.askLevels[0], true
	}
	// 最高买价 ≥ 卖价才交叉
	if len(e.bidLevels) == 0 || e.bidLevels[0] < taker.Price {
		return 0, false
	}
	return e.bidLevels[0], true
}

func (e *MatchingEngine) matchTaker(taker *Order) []Trade {
	var trades []Trade
	for taker.LeavesQuantity > 0 {
		bestPrice, ok := e.bestCrossingPrice(taker)
		if !ok {
			break
		}
		oppositeSide := taker.Side.Opposite()
		opposite, _ := e.bookSide(oppositeSide)
		queue := opposite[bestPrice]
		maker := queue[0] // 同价位先来先成交（FIFO）
		fillQuantity := min(taker.LeavesQuantity, maker.LeavesQuantity)
		makerID := maker.ID
		taker.Fill(fillQuantity)
		maker.Fill(fillQuantity)
		makerFilled := maker.LeavesQuantity == 0
		e.sequence++
		trades = append(trades, Trade{
			TakerOrderID: taker.ID,
			MakerOrderID: makerID,
			TakerSide:    taker.Side,
			Price:        bestPrice, // 成交价取被动方（maker）价格
			Quantity:     fillQuantity,
			Sequence:     e.sequence,
		})
		if makerFilled {
			queue = queue[1:]
			if len(queue) == 0 {
				delete(opposite, bestPrice)
				e.removeLevel(oppositeSide, bestPrice)
			} else {
				opposite[bestPrice] = queue
			}
			delete(e.activeOrders, makerID)
		}
	}
	return trades
}

// MatchingEngineFacade 撮合引擎门面：对外只暴露 Buy / Sell / Cancel / LastTradePrice
// 极简接口，内部装配引擎与默认订阅者（OMS 回报、行情、审计），屏蔽撮合内部复杂度。
type MatchingEngineFacade struct {
	engine     *MatchingEngine
	marketData *MarketDataTradeListener // 与引擎内行情订阅者共享同一指针
}

func NewMatchingEngineFacade() *MatchingEngineFacade {
	engine := NewMatchingEngine()
	marketData := NewMarketDataTradeListener()
	engine.AddTradeListener(NewOmsTradeListener())
	engine.AddTradeListener(marketData)
	engine.AddTradeListener(NewAuditTradeListener())
	return &MatchingEngineFacade{engine: engine, marketData: marketData}
}

func (f *MatchingEngineFacade) Buy(price, quantity uint64) uint64 {
	return f.engine.Submit(SideBuy, price, quantity)
}

func (f *MatchingEngineFacade) Sell(price, quantity uint64) uint64 {
	return f.engine.Submit(SideSell, price, quantity)
}

func (f *MatchingEngineFacade) Cancel(orderID uint64) bool {
	return f.engine.Cancel(orderID)
}

// LastTradePrice 无成交时第二个返回值为 false。
func (f *MatchingEngineFacade) LastTradePrice() (uint64, bool) {
	return f.marketData.LastPrice()
}
