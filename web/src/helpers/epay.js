export const EPAY_METHOD_NAMES = {
  wxpay: '微信支付',
  alipay: '支付宝',
  qqpay: 'QQ',
};

export function epayOfferLabel(channel, translate, subscription = false) {
  const percent = Number(
    subscription
      ? channel.subscription_discount_percent
      : channel.topup_offer_percent,
  );
  const offerType = subscription
    ? percent < 100
      ? 'discount'
      : 'none'
    : channel.topup_offer_type;
  const threshold = Number(
    subscription
      ? channel.subscription_offer_threshold_cny
      : channel.topup_offer_threshold_cny,
  );
  if (offerType === 'none')
    return translate(subscription ? '暂无订阅优惠' : '暂无充值优惠');
  const promotion =
    offerType === 'bonus'
      ? `${translate('赠送')} ${Number((percent - 100).toFixed(6))}%`
      : `${Number((100 - percent).toFixed(6))}% ${translate('折扣')}`;
  return threshold > 0
    ? `${translate('满')} ¥${threshold} ${promotion}`
    : promotion;
}

export function previewEpayAmount(channel, amountCNY, exchangeRate, method) {
  const input = Number(amountCNY || 0);
  const rate = Number(exchangeRate);
  if (!Number.isFinite(rate) || rate <= 0) return null;
  const limits = channel.payment_method_limits?.[method];
  if (!limits) return null;
  const applied = input >= Number(channel.topup_offer_threshold_cny || 0);
  const percent = Number(channel.topup_offer_percent || 100);
  const type = applied ? channel.topup_offer_type : 'none';
  const baseUSD = input / rate;
  const payCNY =
    type === 'discount'
      ? Math.round(((input * percent) / 100 + Number.EPSILON) * 100) / 100
      : input;
  const bonusUSD = type === 'bonus' ? (baseUSD * (percent - 100)) / 100 : 0;
  return {
    payCNY,
    creditUSD: baseUSD + bonusUSD,
    bonusUSD,
    type,
    percentage:
      type === 'bonus'
        ? percent - 100
        : type === 'discount'
          ? 100 - percent
          : 0,
    payable:
      payCNY > 0 &&
      payCNY >= Number(limits.min_pay_cny) &&
      payCNY <= Number(limits.max_pay_cny) &&
      (!(Number(limits.daily_limit_cny) > 0) ||
        (Number.isFinite(Number(limits.daily_remaining_cny)) &&
          payCNY <= Number(limits.daily_remaining_cny))),
  };
}

export function reorderEpayChannels(channels, sourceID, targetID) {
  const sourceIndex = channels.findIndex((channel) => channel.id === sourceID);
  const targetIndex = channels.findIndex((channel) => channel.id === targetID);
  if (sourceIndex < 0 || targetIndex < 0 || sourceIndex === targetIndex)
    return channels;
  const reordered = [...channels];
  const [source] = reordered.splice(sourceIndex, 1);
  reordered.splice(targetIndex, 0, source);
  return reordered;
}
