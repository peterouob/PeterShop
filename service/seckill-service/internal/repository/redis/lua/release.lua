local RELEASE_STATE = {
    RELEASED = 1,
    STOCK_LOST = -4,
}

local slots_key = KEYS[1]
local user_key = KEYS[2]
local meta_key = KEYS[3]

local slot_no = ARGV[1]
local user_id = ARGV[2]
local removed = ARGV[3]


local exists = redis.call('EXISTS', slots_key) == 1
local drained = redis.call('HGET', meta_key, 'drained') == '1'


if not exists and not drained then
    return RELEASE_STATE.STOCK_LOST
end

local slot = tonumber(slot_no)
if slot > 0 then
    redis.call('SADD', slots_key, slot)
    redis.call('HDEL', meta_key, 'drained')
end

if removed == '1' then
    redis.call('SREM', user_key, user_id)
end

return RELEASE_STATE.RELEASED
