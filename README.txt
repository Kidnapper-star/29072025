Архиватор файлов
Программа на Go скачивает файлы (.pdf, .jpeg) по ссылкам и делает zip-архив. Я новичок, поэтому всё максимально просто!
Как запустить

Установите Go с https://golang.org/dl/.
Создайте папку files:mkdir D:\tz\files

Положите main.go и config.json в D:\tz.
Запустите в PowerShell:cd D:\tz
go run .

Как проверить

Откройте новый PowerShell:Start-Process powershell
cd D:\tz

Создайте задачу:Invoke-WebRequest -Method Post -Uri http://localhost:8080/tasks

Увидите что-то вроде: {"id":"20250803194700"}. Запомните id.
Добавьте файлы:$body = '["https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf","https://picsum.photos/200/300.jpg"]' | ConvertTo-Json
Invoke-WebRequest -Method Post -Uri http://localhost:8080/tasks/20250803194700/files -Body $body -ContentType "application/json"

Ответ: {"message":"files ok"}.
Проверьте статус (через 5–10 секунд):Invoke-WebRequest -Method Get -Uri http://localhost:8080/tasks/20250803194700

Ответ: {"id":"...","status":"done","archive":"files/20250803194700.zip"}.
Скачайте архив:Invoke-WebRequest -Method Get -Uri http://localhost:8080/archives/20250803194700.zip -OutFile archive.zip

Распакуйте:Expand-Archive -Path archive.zip -DestinationPath .\archive_contents
dir .\archive_contents

Если не работает

Пишет "cant create folder"? Создайте files вручную или запустите PowerShell как админ:Start-Process powershell -Verb runAs

Не соединяется? Проверьте, работает ли сервер, и порт 8080.
Пишет "invalid format"? Проверьте JSON в $body.
Пишет "only .pdf and .jpeg"? Берите ссылки на .pdf или .jpeg.
Порт занят? Поменяйте "port" в config.json на 9090.

Ссылки для теста

PDF: https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf
JPEG: https://picsum.photos/200/300.jpg
Неправильная: https://picsum.photos/200/300.png
