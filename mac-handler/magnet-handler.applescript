-- Обработчик magnet:-ссылок для macOS
-- Ловит магнет из браузера/PWA (window.location = magnet:…) и отправляет его
-- на удалённый Transmission через ssh + transmission-remote.
--
-- Установка: см. install.sh (osacompile + регистрация схемы magnet: в LaunchServices).

on open location theMagnet
	try
		set cmd to "ssh -o BatchMode=yes -o ConnectTimeout=8 micro transmission-remote 127.0.0.1:9091 -a " & quoted form of theMagnet
		set res to do shell script cmd
		display notification "Раздача отправлена на micro" with title "Lampa → Transmission"
	on error errMsg
		display notification errMsg with title "Lampa → Transmission: ошибка"
	end try
end open location

-- запуск двойным кликом (для теста без магнета)
on run
	display notification "Я обработчик magnet:-ссылок. Работаю через ssh micro + transmission-remote." with title "Lampa → Transmission"
end run
