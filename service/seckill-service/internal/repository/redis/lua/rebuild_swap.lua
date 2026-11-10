-- Swap a rebuilt users set in and overwrite stock / meta, all or nothing.
-- KEYS: 1=users 2=users_tmp 3=stock 4=meta
-- ARGV: 1=stock 2=start_ms 3=end_ms 4=expected_user_count 5=sealed ("1" or "0")
-- SCARD(users_tmp) ~= expected_user_count -> redis.error_reply('REBUILD_TMP_MISMATCH'), touch nothing
-- sealed is a one-way latch: set it when ARGV[5] == "1", never remove it
-- returns 1
local usersKey = KEYS[1]
local userTmpKey = KEYS[2]
local stockKey = KEYS[3]
local metaKey = KEYS[4]

local stock = ARGV[1]
local startMs = ARGV[2]
local endMs = ARGV[3]
local expectNum = tonumber(ARGV[4])
local sealed = ARGV[5]

local F = {
    START = 'start_ms',
    END = 'end_ms',
    SEALED = 'sealed',
}

if expectNum == nil or expectNum < 0 or expectNum % 1 ~= 0 then
    return redis.error_reply('REBUILD_BADARG expectNum=' .. tostring(ARGV[4]))
end

if sealed ~= '1' and sealed ~= '0' then
    return redis.error_reply('REBUILD_BADARG sealed=' .. tostring(sealed))
end

local actual = redis.call('SCARD', userTmpKey)
if actual ~= expectNum then
    return redis.error_reply('REBUILD_TMP_MISMATCH expected=' .. expectNum .. ' got=' .. actual)
end

redis.call('UNLINK', usersKey)
if expectNum > 0 then
    redis.call('RENAME', userTmpKey, usersKey)
    redis.call('PERSIST', usersKey)
end

redis.call('SET', stockKey, stock)

redis.call('HSET', metaKey, F.START, startMs, F.END, endMs)
if sealed == '1' then
    redis.call('HSET', metaKey, F.SEALED, '1')
end

return 1
