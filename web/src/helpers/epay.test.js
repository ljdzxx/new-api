import { expect, test } from 'bun:test';
import { epayOfferLabel, previewEpayAmount, reorderEpayChannels } from './epay';

const channel = {
  payment_method_limits: {
    wxpay: { min_pay_cny: '1', max_pay_cny: '5000', daily_limit_cny: '0' },
    alipay: {
      min_pay_cny: '5',
      max_pay_cny: '100',
      daily_limit_cny: '200',
      daily_remaining_cny: '50',
    },
  },
  topup_offer_type: 'bonus',
  topup_offer_percent: '102',
  topup_offer_threshold_cny: '100',
  subscription_discount_percent: '98',
  subscription_offer_threshold_cny: '73',
};

test('wallet bonus preview divides by the system CNY/USD rate', () => {
  const quote = previewEpayAmount(channel, 100, 5, 'wxpay');
  expect(quote.payCNY).toBe(100);
  expect(quote.creditUSD).toBeCloseTo(20.4);
  expect(quote.bonusUSD).toBeCloseTo(0.4);
  expect(quote.percentage).toBe(2);
  expect(previewEpayAmount(channel, 99.99, 5, 'wxpay').bonusUSD).toBe(0);
  expect(previewEpayAmount(channel, 100, 7.3, 'wxpay').creditUSD).toBeCloseTo(
    (100 / 7.3) * 1.02,
  );
});

test('discount preview preserves credit and checks actual-payment limits', () => {
  const discounted = {
    ...channel,
    topup_offer_type: 'discount',
    topup_offer_percent: '98',
    payment_method_limits: {
      wxpay: { min_pay_cny: '98', max_pay_cny: '98', daily_limit_cny: '0' },
    },
  };
  const quote = previewEpayAmount(discounted, 100, 5, 'wxpay');
  expect(quote).toMatchObject({
    payCNY: 98,
    bonusUSD: 0,
    payable: true,
  });
  expect(quote.creditUSD).toBeCloseTo(20);
  expect(previewEpayAmount(discounted, 99.99, 5, 'wxpay').payable).toBe(false);
});

test('wallet preview does not fall back to a made-up exchange rate', () => {
  for (const rate of [undefined, null, '', 0, -1, NaN, Infinity]) {
    expect(previewEpayAmount(channel, 100, rate, 'wxpay')).toBeNull();
  }
});

test('previews use the selected method single-payment and daily limits', () => {
  expect(previewEpayAmount(channel, 50, 5, 'alipay').payable).toBe(true);
  expect(previewEpayAmount(channel, 50.01, 5, 'alipay').payable).toBe(false);
  expect(previewEpayAmount(channel, 500, 5, 'wxpay').payable).toBe(true);
  expect(previewEpayAmount(channel, 4.99, 5, 'alipay').payable).toBe(false);
  expect(previewEpayAmount(channel, 50, 5, 'qqpay')).toBeNull();
  const discounted = {
    ...channel,
    topup_offer_type: 'discount',
    topup_offer_percent: '98',
    topup_offer_threshold_cny: '0',
  };
  expect(previewEpayAmount(discounted, 51.02, 5, 'alipay').payable).toBe(true);
  expect(previewEpayAmount(discounted, 51.03, 5, 'alipay').payable).toBe(false);
});

test('channel sorting moves in either direction without mutating the source', () => {
  const channels = [{ id: 1 }, { id: 2 }, { id: 3 }];
  expect(reorderEpayChannels(channels, 1, 3).map((item) => item.id)).toEqual([
    2, 3, 1,
  ]);
  expect(reorderEpayChannels(channels, 3, 1).map((item) => item.id)).toEqual([
    3, 1, 2,
  ]);
  expect(channels.map((item) => item.id)).toEqual([1, 2, 3]);
  expect(reorderEpayChannels(channels, 2, 2)).toBe(channels);
  expect(reorderEpayChannels(channels, -1, 1)).toBe(channels);
  expect(reorderEpayChannels(channels, 1, -1)).toBe(channels);
});

test('wallet and subscription labels use independent offers and thresholds', () => {
  const translate = (key) => key;
  expect(epayOfferLabel(channel, translate)).toBe('满 ¥100 赠送 2%');
  expect(epayOfferLabel(channel, translate, true)).toBe('满 ¥73 2% 折扣');
  expect(
    epayOfferLabel(
      { ...channel, subscription_discount_percent: '100' },
      translate,
      true,
    ),
  ).toBe('暂无订阅优惠');
});
