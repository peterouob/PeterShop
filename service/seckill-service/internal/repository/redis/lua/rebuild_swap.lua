local users = KEYS[1]
local users_tmp = KEYS[2]
local slots = KEYS[3]
local slots_tmp = KEYS[4]
local meta = KEYS[5]

local startMs = ARGV[1]
local endMs = ARGV[2]
local sealed = ARGV[5]

-- @enum FlagForRedis
local F = {
    START = 'start_ms',
    END = 'end_ms',
    SEALED = 'sealed',
    DRAINED = 'drained',
}

-- @enum RebuildResult
local REBUILD = {
    BAD_ARG = 'REBUILD_BAD_ARG',
    TMP_MISMATCH = 'REBUILD_TMP_MISMATCH',
    OK = 1,
}

-- @param s string
-- @return integer? count
local function parse_count(s)
    local c = tonumber(s)
    if not c or c < 0 or c ~= math.floor(c) then
        return nil
    end
    return c
end

local expect_users = parse_count(ARGV[3])
local expect_slots = parse_count(ARGV[4])

if not expect_users then
    return redis.error_reply(REBUILD.BAD_ARG .. ' expected_users=' .. tostring(ARGV[3]))
end

if not expect_slots then
    return redis.error_reply(REBUILD.BAD_ARG .. ' expected_slots=' .. tostring(ARGV[4]))
end

if sealed ~= '1' and sealed ~= '0' then
    return redis.error_reply(REBUILD.BAD_ARG .. ' sealed=' .. tostring(sealed))
end

local actual_slots = redis.call('SCARD', slots_tmp)
if actual_slots ~= expect_slots then
    return redis.error_reply(REBUILD.TMP_MISMATCH .. ' expected=' .. expect_slots .. ' got=' .. actual_slots)
end

local actual_users = redis.call('SCARD', users_tmp)
if actual_users ~= expect_users then
    return redis.error_reply(REBUILD.TMP_MISMATCH .. ' expected=' .. expect_users .. ' got=' .. actual_users)
end

redis.call('UNLINK', slots)
if expect_slots > 0 then
    redis.call('RENAME', slots_tmp, slots)
    redis.call('PERSIST', slots)
    redis.call('HDEL', meta, F.DRAINED)
else
    redis.call('HSET', meta, F.DRAINED, '1')
end

redis.call('UNLINK', users)
if expect_users > 0 then
    redis.call('RENAME', users_tmp, users)
    redis.call('PERSIST', users)
end

redis.call('HSET', meta, F.START, startMs, F.END, endMs)
if sealed == '1' then
    redis.call('HSET', meta, F.SEALED, '1')
end

return REBUILD.OK
