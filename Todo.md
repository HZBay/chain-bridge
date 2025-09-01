transactions、user_balances user_accounts  user_id 类型从uuid改为字符串， 接口不要做uuid校验， 相应的接口、代码同步修改

获取支付事件统计
GET /monitoring/payment-events/stats
{
  "total_listeners": 3,
  "healthy_listeners": 3,
  "total_events_processed": 1245,
  "total_errors": 2,
  "listeners": {
    "chain_1": {
      "chain_id": 1,
      "payment_address": "0x...",
      "last_processed_block": 18600000,
      "total_events": 856,
      "total_errors": 1
    }
  }
}

重新加载配置
POST /monitoring/payment-events/reload