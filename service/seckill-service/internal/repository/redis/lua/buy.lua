local BUY_STATE = {
    NOT_PREHEATED = -1,
    STOCK_LOST = -4,
    SOLD_OUT = 0,
    DUPLICATE = 2,
    PASS = 1,
}

local meta_key = KEYS[1]
local slots_key = KEYS[2]

local m = redis.call('HMGET', meta_key, 'start_ms', 'end_ms')
if not m[1] or not m[2] then
    return { BUY_STATE.NOT_PREHEATED, 0 }
end

local start_ms = tonumber(m[1])
local end_ms = tonumber(m[2])

if redis.call('HEXISTS', meta_key, 'sealed') == 1 then
    return { WINDOW.SEALED, 0 }
end

local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)

local state = window_state(now, start_ms, end_ms)
if state == WINDOW.SEALED then
    redis.call('HSET', meta_key, 'sealed', 1)
    return { WINDOW.SEALED, 0 }
elseif state == WINDOW.NOT_OPEN then
    return { WINDOW.NOT_OPEN, 0 }
end

local users_key = KEYS[3]
local user_id = ARGV[1]

if redis.call('SISMEMBER', users_key, user_id) == 1 then
    return { BUY_STATE.DUPLICATE, 0 }
end

local slot = redis.call('SPOP', slots_key)
if not slot then
    if redis.call('HGET', meta_key, 'drained') == '1' then
        return { BUY_STATE.SOLD_OUT, 0 }
    end
    return {BUY_STATE.STOCK_LOST, 0}
end

if redis.call('EXISTS', slots_key) == 0 then
    redis.call('HSET', meta_key, 'drained', '1')
end

redis.call('SADD', users_key, user_id)

return { BUY_STATE.PASS, tonumber(slot) }