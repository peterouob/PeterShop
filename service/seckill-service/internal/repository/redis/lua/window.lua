#!lua

local WINDOW = {
    OPEN = 0,
    NOT_OPEN = -2,
    SEALED = -3,
}

local function window_state(now, startMs, endMs)
    now, startMs, endMs = tonumber(now), tonumber(startMs), tonumber(endMs)

    if now < startMs then
        return WINDOW.NOT_OPEN
    end
    if now >= endMs then
        return WINDOW.SEALED
    end
    return WINDOW.OPEN
end
