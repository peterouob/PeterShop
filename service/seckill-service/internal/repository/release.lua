local RELEASE_STATE = {
    RELEASED = 1,
    NOT_HELD = 0,
    STOCK_LOST = -4,
}

local stock_key = KEYS[1]
local users_key = KEYS[2]
local user_id = ARGV[1]

if redis.call('EXISTS', stock_key) == 0 then
    return RELEASE_STATE.STOCK_LOST
end

if redis.call('SREM', users_key, user_id) == 0 then
    return RELEASE_STATE.NOT_HELD
end

redis.call('INCR', stock_key)
return RELEASE_STATE.RELEASED