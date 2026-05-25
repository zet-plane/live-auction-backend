package cache

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
)

const bidLuaScript = `
local state_key   = KEYS[1]
local ranking_key = KEYS[2]
local names_key   = KEYS[3]
local idem_key    = KEYS[4]

local user_id     = ARGV[1]
local user_name   = ARGV[2]
local bid_id      = ARGV[3]
local price       = tonumber(ARGV[4])
local bid_incr    = tonumber(ARGV[5])
local price_cap   = tonumber(ARGV[6])
local ext_trig    = tonumber(ARGV[7])
local ext_sec     = tonumber(ARGV[8])
local max_ext_cnt = tonumber(ARGV[9])
local max_ext_tot = tonumber(ARGV[10])
local now_unix    = tonumber(ARGV[11])
local idem_ttl    = tonumber(ARGV[12])

local existing = redis.call('GET', idem_key)
if existing then
  local raw = redis.call('HGETALL', state_key)
  local m = {}
  for i = 1, #raw, 2 do m[raw[i]] = raw[i+1] end
  return {1, existing, tonumber(m['current_price'] or 0), m['leader_user_id'] or '', tonumber(m['end_time_unix'] or 0), 0, 0, ''}
end

local raw = redis.call('HGETALL', state_key)
if #raw == 0 then return {2,'',0,'',0,0,0,''} end
local s = {}
for i = 1, #raw, 2 do s[raw[i]] = raw[i+1] end

local cur_price = tonumber(s['current_price'] or 0)
local end_unix  = tonumber(s['end_time_unix']  or 0)
local ext_cnt   = tonumber(s['extend_count']   or 0)
local ext_tot   = tonumber(s['total_extended_sec'] or 0)
local bid_cnt   = tonumber(s['bid_count']       or 0)
local part_cnt  = tonumber(s['participant_count'] or 0)
local prev_leader = s['leader_user_id'] or ''

if now_unix >= end_unix then return {2,'',0,'',0,0,0,''} end
if price <= cur_price   then return {3,'',0,'',0,0,0,''} end
if (price - cur_price) % bid_incr ~= 0 then return {4,'',0,'',0,0,0,''} end

local prev_score = redis.call('ZSCORE', ranking_key, user_id)
if not prev_score then part_cnt = part_cnt + 1 end

redis.call('ZADD', ranking_key, 'GT', price, user_id)
redis.call('HSET', names_key, user_id, user_name)
bid_cnt = bid_cnt + 1

local is_extended = 0
local remaining = end_unix - now_unix
if remaining <= ext_trig and ext_cnt < max_ext_cnt and (ext_tot + ext_sec) <= max_ext_tot then
  end_unix  = end_unix  + ext_sec
  ext_cnt   = ext_cnt   + 1
  ext_tot   = ext_tot   + ext_sec
  is_extended = 1
end

redis.call('HSET', state_key,
  'current_price',      price,
  'leader_user_id',     user_id,
  'end_time_unix',      end_unix,
  'bid_count',          bid_cnt,
  'participant_count',  part_cnt,
  'is_extended',        is_extended,
  'extend_count',       ext_cnt,
  'total_extended_sec', ext_tot)

redis.call('SET', idem_key, bid_id, 'EX', idem_ttl)

local is_capped = 0
if price_cap > 0 and price >= price_cap then is_capped = 1 end

return {0, bid_id, price, user_id, end_unix, is_extended, is_capped, prev_leader}
`

var bidScript = redis.NewScript(bidLuaScript)

func rankingKey(itemID string) string {
	return "auction:item:" + itemID + ":ranking"
}

func bidderNamesKey(itemID string) string {
	return "auction:item:" + itemID + ":bidder_names"
}

func idempotencyKey(itemID, key string) string {
	return "auction:item:" + itemID + ":idempotency:" + key
}

func (c *RedisCache) PlaceBidLua(ctx context.Context, itemID string, args BidLuaArgs) (*BidLuaResult, error) {
	keys := []string{
		itemStateKey(itemID),
		rankingKey(itemID),
		bidderNamesKey(itemID),
		idempotencyKey(itemID, args.IdempotencyKey),
	}
	argv := []interface{}{
		args.UserID,
		args.UserName,
		args.BidID,
		strconv.FormatInt(args.Price, 10),
		strconv.FormatInt(args.BidIncrement, 10),
		strconv.FormatInt(args.PriceCap, 10),
		strconv.Itoa(args.ExtendTriggerSec),
		strconv.Itoa(args.AutoExtendSec),
		strconv.Itoa(args.MaxExtendCount),
		strconv.Itoa(args.MaxTotalExtendSec),
		strconv.FormatInt(args.NowUnix, 10),
		strconv.Itoa(args.IdempotencyTTL),
	}

	res, err := bidScript.Run(ctx, c.client, keys, argv...).Slice()
	if err != nil {
		return nil, err
	}
	if len(res) < 8 {
		return nil, fmt.Errorf("lua result length unexpected: %d", len(res))
	}

	toI64 := func(v interface{}) int64 { n, _ := v.(int64); return n }
	toStr := func(v interface{}) string { s, _ := v.(string); return s }

	return &BidLuaResult{
		Code:             int(toI64(res[0])),
		BidID:            toStr(res[1]),
		CurrentPrice:     toI64(res[2]),
		LeaderUserID:     toStr(res[3]),
		EndTimeUnix:      toI64(res[4]),
		IsExtended:       toI64(res[5]) == 1,
		IsCapped:         toI64(res[6]) == 1,
		PrevLeaderUserID: toStr(res[7]),
	}, nil
}

func (c *RedisCache) GetRanking(ctx context.Context, itemID string, offset, limit int) ([]dto.BidderPrice, error) {
	if limit <= 0 {
		return nil, nil
	}
	members, err := c.client.ZRevRangeWithScores(ctx, rankingKey(itemID), int64(offset), int64(offset+limit-1)).Result()
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil
	}
	userIDs := make([]string, len(members))
	for i, m := range members {
		userIDs[i] = m.Member.(string)
	}
	names, err := c.client.HMGet(ctx, bidderNamesKey(itemID), userIDs...).Result()
	if err != nil {
		return nil, err
	}
	entries := make([]dto.BidderPrice, len(members))
	for i, m := range members {
		name := ""
		if names[i] != nil {
			name, _ = names[i].(string)
		}
		entries[i] = dto.BidderPrice{
			UserID:   userIDs[i],
			UserName: name,
			Price:    int64(m.Score),
		}
	}
	return entries, nil
}
