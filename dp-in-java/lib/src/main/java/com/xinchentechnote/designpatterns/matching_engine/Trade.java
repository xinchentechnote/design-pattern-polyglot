package com.xinchentechnote.designpatterns.matching_engine;

/**
 * 成交回报：主动单（taker，新进订单）吃掉被动单（maker，簿内挂单）。
 * 成交价取被动方价格，符合连续竞价"价格优先、时间优先"的撮合惯例。
 */
public final class Trade {
    private final long takerOrderId;
    private final long makerOrderId;
    private final Side takerSide;
    private final long price;
    private final long quantity;
    private final long sequence;

    public Trade(long takerOrderId, long makerOrderId, Side takerSide,
                 long price, long quantity, long sequence) {
        this.takerOrderId = takerOrderId;
        this.makerOrderId = makerOrderId;
        this.takerSide = takerSide;
        this.price = price;
        this.quantity = quantity;
        this.sequence = sequence;
    }

    public long getTakerOrderId() {
        return takerOrderId;
    }

    public long getMakerOrderId() {
        return makerOrderId;
    }

    public Side getTakerSide() {
        return takerSide;
    }

    public long getPrice() {
        return price;
    }

    public long getQuantity() {
        return quantity;
    }

    public long getSequence() {
        return sequence;
    }
}
