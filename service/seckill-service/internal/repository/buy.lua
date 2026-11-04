local BUY_STATE = {
    NOT_PREHEATED = -1,
    STOCK_LOST = -4,
    SOLD_OUT = 0,
    DUPLICATE = 2,
    PASS = 1,
}

local meta_key = KEYS[1]
local stock_key = KEYS[2]

local m = redis.call('HMGET', meta_key, 'start_ms', 'end_ms')
if not m[1] or not m[2] then
    return BUY_STATE.NOT_PREHEATED
end

local start_ms = tonumber(m[1])
local end_ms = tonumber(m[2])

if redis.call('HEXISTS', meta_key, 'sealed') == 1 then
    return WINDOW.SEALED
end

local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)

local state = window_state(now, start_ms, end_ms)
if state == WINDOW.SEALED then
    redis.call('HSET', meta_key, 'sealed', 1)
    return WINDOW.SEALED
elseif state == WINDOW.NOT_OPEN then
    return WINDOW.NOT_OPEN
end

local users_key = KEYS[3]
local user_id = ARGV[1]

if redis.call('SISMEMBER', users_key, user_id) == 1 then
    return BUY_STATE.DUPLICATE
end

local stock = tonumber(redis.call('GET', stock_key))
if not stock then
    return BUY_STATE.STOCK_LOST
end
if stock <= 0 then
    return BUY_STATE.SOLD_OUT
end

redis.call('DECR', stock_key)
redis.call('SADD', users_key, user_id)

return BUY_STATE.PASS
