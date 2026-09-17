local stock = KEYS[1]
local users = KEYS[2]
local user_id = ARGV[1]

local ips = KEYS[3]
local ip = ARGV[2]

local user_exist = redis.call('sismember', users, user_id)
if user_exist == 1 then
    return 2
end

local stock = redis.call('get', stock)
if not stock then
    return -1
end

if tonumber(stock) <= 0 then
    return 0
else
    redis.call('decr', stock)
    redis.call('sadd', users, user_id)
    redis.call('sadd', ips, ip)
    return 1
end

