package com.xinchentechnote.designpatterns.matching_engine;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

public class MatchingEngineFacadeTest {

    @Test
    public void testTradeThroughFacade() {
        MatchingEngineFacade facade = new MatchingEngineFacade();
        facade.sell(100, 5);
        facade.buy(101, 5);
        assertEquals(Long.valueOf(100), facade.lastTradePrice());
    }

    @Test
    public void testCancelThroughFacade() {
        MatchingEngineFacade facade = new MatchingEngineFacade();
        long sellId = facade.sell(100, 5);
        assertTrue(facade.cancel(sellId));

        facade.buy(100, 5);
        assertNull(facade.lastTradePrice());  // 已撤单，无成交
    }
}
