# live-auction-backend

* 保证不同业务状态强一致性：
1. 是有订单的状态 分为 pending success expired cancel
2. 是有支付的状态 分为 success
3. 是出价前的保证金的状态记录 `pending`、`paid`、`refunded`、`forfeited`
* 出价，排行榜的跨存储最终一致性：
1. 主要是 Redis 和 mysql
2. 最实时的操作放 reids ，后续异步落库
3. 链路1： 出价 -> 排行榜得变 -> 后台记录得变
4. 链路2:   货品上架，当前room上架货品得变
5. 链路3:   出价前需先支付保证金
* 幂等性检查
1. 不能重复创建相同订单
2. 不能重复支付
3. 不能重复交保证金
4. 不能重复上架商品 --> 也就是商品列表中的商品不能重复  
