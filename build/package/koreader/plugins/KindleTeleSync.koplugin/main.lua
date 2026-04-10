local InfoMessage = require("ui/widget/infomessage")
local UIManager = require("ui/uimanager")
local TextViewer = require("ui/widget/textviewer")
local WidgetContainer = require("ui/widget/container/widgetcontainer")
local NetworkMgr = require("ui/network/manager")
local _ = require("gettext")
local ffiutil = require("ffi/util")
local T = ffiutil.template
local QRWidget = require("ui/widget/qrwidget")


local KindleTeleSync = WidgetContainer:extend {
    name = "KindleTeleSync",
    is_doc_only = false,
}

function KindleTeleSync:init()
    self.ui.menu:registerToMainMenu(self)
end

local function is_wifi_enabled()
    local f = io.popen("lipc-get-prop com.lab126.cmd wirelessEnable")
    if not f then return false end
    local result = f:read("*a")
    f:close()
    return result
end

local function do_sync()
    if is_wifi_enabled() == 0 then
        UIManager:show(InfoMessage:new {
            text = _("Wi-Fi выключен. Пожалуйста, включите Wi-Fi и повторите попытку."),
            timeout = 4
        })
        return
    end

    UIManager:show(InfoMessage:new {
        text = T(_("KindleTeleSync started.")),
        timeout = 2,
    })
    local cmd = "cd /mnt/us/extensions/KindleTeleSync && ./kindle_sync_d 2>&1"
    local handle = io.popen(cmd)
    local output
    if handle ~= nil then
        output = handle:read("*a")
        handle:close()
    end
    if not output or output == "" then
        output = "Синхронизация завершена без вывода."
    end

    UIManager:show(TextViewer:new {
        text = output,
        title = _("KindleTeleSync"),
    })
end

local function do_update()
    if is_wifi_enabled() == 0 then
        UIManager:show(InfoMessage:new {
            text = _("Wi-Fi выключен. Пожалуйста, включите Wi-Fi и повторите попытку."),
            timeout = 4
        })
        return
    end

    UIManager:show(InfoMessage:new {
        text = T(_("KindleTeleSync update started.")),
        timeout = 2,
    })
    local cmd = "cd /mnt/us/extensions/KindleTeleSync && ./updater 2>&1"
    local handle = io.popen(cmd)
    local output
    if handle ~= nil then
        output = handle:read("*a")
        handle:close()
    end

    if not output or output == "" then
        output = "Операция завершена без вывода."
    end

    UIManager:show(TextViewer:new {
        text = output,
        title = _("KindleTeleSync"),
    })
end

local function launch_web_config()
    if is_wifi_enabled() == 0 then
        UIManager:show(InfoMessage:new {
            text = _("Wi-Fi выключен. Пожалуйста, включите Wi-Fi и повторите попытку."),
            timeout = 4
        })
        return
    end

    os.execute("cd /mnt/us/extensions/KindleTeleSync && ./webconfig &")

    local ip = io.popen("ip route get 1 | awk '{print $7}'"):read("*l")
    local url = "http://" .. ip .. ":8880"
    local qr_image = QRWidget:new {
        text = url,
        width = 350,
        height = 350,
        scale_factor = 1
    }

    local infomessage = InfoMessage:new {
        text = _("Отсканируй QRCode для изменения настроек плагина или перейди по ссылке: \n\n" .. url .. "\nСервер работает пока отображается это сообщение."),
        image = qr_image.image,
        alignment = "right",
        dismiss_callback = function() os.execute("killall webconfig") end
    }

    UIManager:show(infomessage)
end

function KindleTeleSync:addToMainMenu(menu_items)
    menu_items.telesync_action = {
        text = _("KindleTeleSync"),
        keep_menu_open = true,
        sub_item_table = {
            {
                text = _("Запустить синхронизацию"),
                keep_menu_open = true,
                callback = function()
                    NetworkMgr:runWhenOnline(function() do_sync() end)
                end
            },
            {
                text = _("Запустить сервер настроек (с внешнего устройства)"),
                keep_menu_open = true,
                callback = function() return launch_web_config() end,
            },
            {
                text = _("Проверка обновлений"),
                keep_menu_open = true,
                callback = function() return do_update() end,
            },
        }

    }
end

return KindleTeleSync
