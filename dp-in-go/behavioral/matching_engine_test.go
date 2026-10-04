package behavioral

import (
	"strings"
	"testing"
)

func TestPricePriority(t *testing.T) {
	engine := NewMatchingEngine()
	engine.Submit(SideSell, 101, 5) // 高价档
	engine.Submit(SideSell, 100, 5) // 低价档：价格优先，应先被吃
	oms := NewOmsTradeListener()
	engine.AddTradeListener(oms)

	engine.Submit(SideBuy, 105, 5)

	executions := oms.Executions()
	if len(executions) != 1 {
		t.Fatalf("price priority failed: got %d trades, want 1", len(executions))
	}
	if executions[0].Price != 100 {
		t.Errorf("price priority failed: got price %d, want 100", executions[0].Price)
	}
	if executions[0].Quantity != 5 {
		t.Errorf("price priority failed: got qty %d, want 5", executions[0].Quantity)
	}
}

func TestTimePriority(t *testing.T) {
	engine := NewMatchingEngine()
	first := engine.Submit(SideSell, 100, 3)  // 同价位先到
	second := engine.Submit(SideSell, 100, 4) // 同价位后到
	oms := NewOmsTradeListener()
	engine.AddTradeListener(oms)

	engine.Submit(SideBuy, 100, 5)

	executions := oms.Executions()
	if len(executions) != 2 {
		t.Fatalf("time priority failed: got %d trades, want 2", len(executions))
	}
	if executions[0].MakerOrderID != first || executions[0].Quantity != 3 {
		t.Errorf("time priority failed: first trade = (maker %d, qty %d), want (maker %d, qty 3)",
			executions[0].MakerOrderID, executions[0].Quantity, first)
	}
	if executions[1].MakerOrderID != second || executions[1].Quantity != 2 {
		t.Errorf("time priority failed: second trade = (maker %d, qty %d), want (maker %d, qty 2)",
			executions[1].MakerOrderID, executions[1].Quantity, second)
	}
}

func TestPartialFill(t *testing.T) {
	engine := NewMatchingEngine()
	sellID := engine.Submit(SideSell, 100, 10)
	oms := NewOmsTradeListener()
	engine.AddTradeListener(oms)

	engine.Submit(SideBuy, 100, 7)
	engine.Submit(SideBuy, 100, 2)
	engine.Submit(SideBuy, 100, 1)

	executions := oms.Executions()
	if len(executions) != 3 {
		t.Fatalf("partial fill failed: got %d trades, want 3", len(executions))
	}
	if executions[0].Quantity != 7 || executions[1].Quantity != 2 {
		t.Errorf("partial fill failed: quantities = %d, %d; want 7, 2",
			executions[0].Quantity, executions[1].Quantity)
	}
	if engine.Cancel(sellID) {
		t.Error("partial fill failed: fully filled order should not be cancellable")
	}
}

func TestNoTradeWhenNoCross(t *testing.T) {
	engine := NewMatchingEngine()
	oms := NewOmsTradeListener()
	engine.AddTradeListener(oms)

	engine.Submit(SideBuy, 99, 5)
	engine.Submit(SideSell, 101, 5)

	if len(oms.Executions()) != 0 {
		t.Errorf("no cross failed: got %d trades, want 0", len(oms.Executions()))
	}
}

func TestCancelRemovesOrderFromBook(t *testing.T) {
	engine := NewMatchingEngine()
	sellID := engine.Submit(SideSell, 100, 5)
	if !engine.Cancel(sellID) {
		t.Fatal("cancel failed: first cancel should succeed")
	}
	if engine.Cancel(sellID) {
		t.Error("cancel failed: repeated cancel should fail")
	}

	oms := NewOmsTradeListener()
	engine.AddTradeListener(oms)
	engine.Submit(SideBuy, 100, 5)
	if len(oms.Executions()) != 0 {
		t.Errorf("cancel failed: cancelled order should not trade, got %d trades", len(oms.Executions()))
	}
}

func TestTakerCanSweepMultipleLevels(t *testing.T) {
	engine := NewMatchingEngine()
	engine.Submit(SideSell, 101, 3)
	engine.Submit(SideSell, 100, 4)
	oms := NewOmsTradeListener()
	engine.AddTradeListener(oms)

	engine.Submit(SideBuy, 101, 6)

	executions := oms.Executions()
	if len(executions) != 2 {
		t.Fatalf("sweep failed: got %d trades, want 2", len(executions))
	}
	if executions[0].Price != 100 || executions[0].Quantity != 4 {
		t.Errorf("sweep failed: first trade = (price %d, qty %d), want (100, 4)",
			executions[0].Price, executions[0].Quantity)
	}
	if executions[1].Price != 101 || executions[1].Quantity != 2 {
		t.Errorf("sweep failed: second trade = (price %d, qty %d), want (101, 2)",
			executions[1].Price, executions[1].Quantity)
	}
}

func TestMultipleListenersNotified(t *testing.T) {
	engine := NewMatchingEngine()
	oms := NewOmsTradeListener()
	marketData := NewMarketDataTradeListener()
	audit := NewAuditTradeListener()
	engine.AddTradeListener(oms)
	engine.AddTradeListener(marketData)
	engine.AddTradeListener(audit)

	engine.Submit(SideSell, 100, 5)
	engine.Submit(SideBuy, 100, 2)

	if len(oms.Executions()) != 1 {
		t.Errorf("listeners failed: oms got %d trades, want 1", len(oms.Executions()))
	}
	if price, ok := marketData.LastPrice(); !ok || price != 100 {
		t.Errorf("listeners failed: last price = (%d, %v), want (100, true)", price, ok)
	}
	if marketData.TotalVolume() != 2 {
		t.Errorf("listeners failed: total volume = %d, want 2", marketData.TotalVolume())
	}
	logs := audit.Logs()
	if len(logs) != 1 || !strings.Contains(logs[0], "taker=2 maker=1") {
		t.Errorf("listeners failed: audit logs = %v", logs)
	}
}

func TestTradeThroughFacade(t *testing.T) {
	facade := NewMatchingEngineFacade()
	facade.Sell(100, 5)
	facade.Buy(101, 5)
	if price, ok := facade.LastTradePrice(); !ok || price != 100 {
		t.Errorf("facade failed: last price = (%d, %v), want (100, true)", price, ok)
	}
}

func TestCancelThroughFacade(t *testing.T) {
	facade := NewMatchingEngineFacade()
	sellID := facade.Sell(100, 5)
	if !facade.Cancel(sellID) {
		t.Fatal("facade failed: cancel should succeed")
	}
	facade.Buy(100, 5)
	if _, ok := facade.LastTradePrice(); ok {
		t.Error("facade failed: cancelled order should not produce last price")
	}
}
