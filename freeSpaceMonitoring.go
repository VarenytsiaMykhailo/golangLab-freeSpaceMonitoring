package main

//lab.posevin.com:22 test1 12345678990test1 lab.posevin.com:22 test2 12345678990test2 lab.posevin.com:22 test3 12345678990test3
//127.0.0.1:22 login password 123.12.12.31:22 admin asd 255.55.55.55:62 log pas lab.posevin.com:22 abra codabra007
import (
	"fmt"
	"golang.org/x/crypto/ssh"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	pageTop = `
	<!DOCTYPE HTML><html>
	<head>
		<style>.error{color:#FF0000;}</style>
	</head>
	<title>Space monitoring</title>
	<body>
		<h3>Space monitoring</h3>
		<p>Computes free space on the hard drive of various hosts</p>
		<p>The program establishes an ssh connection with the entered hosts and uses password authentication</p>
	`
	form = `
	<form action="/" method="POST">
	<label for="hosts">Enter the hosts and login, password for each host after them (space-separated) ex: "lab.posevin.com:22 test1 123 lab.posevin.com:22 test2 123 lab.posevin.com:22 test3 123":</label><br><br>
	<input type="text" name="hosts" style="width: 100%;"><br><br>
	<input type="submit" value="monitor">
	</form><br><br>
	`
	pageBottom = `</body></html>`
	anError    = `<p class="error">%s</p>`
)

type host struct {
	addr     string
	login    string
	password string
}

var hosts []*host

func main() {
	http.HandleFunc("/", homePage)
	if err := http.ListenAndServe(":3000", nil); err != nil {
		log.Fatal("failed to start server", err)
	}
}

func homePage(writer http.ResponseWriter, request *http.Request) {
	hosts = hosts[:0] //обнуляем слайс hosts, чтобы на сайте можно было повторно вбивать данные
	err := request.ParseForm() //Анализ аргументов. Должен вызываться перед записью в ответ
	fmt.Fprint(writer, pageTop, form)
	if err != nil {
		fmt.Fprintf(writer, anError, err)
	} else {
		if message, ok := processRequest(request); ok {
			for _, v := range hosts { // отладка. Выводим hosts
				fmt.Println(v)
			}
			resultsFromSshServer := make(chan string, 10) // будем записывать результаты работы ssh клиента в буферизированный канал строк
			for i, _ := range hosts { // запустим по одной goroutine на каждый хост
				go func(host *host) {
					resultsFromSshServer <- sshClient(host)
				}(hosts[i])
			}
			timeout := time.After(5 * time.Second) // таймаут на 5 сек (через 5 сек после ожидания данных перестанем забирать результаты из канала ответов от ssh серверов)
			//var results []string
			fmt.Fprint(writer, `<table border="2"><tr>`) //посылаем на сайт начало таблицы
			for i := 0; i < len(hosts); i++ { //формируем результаты из канала
				select { //по мере появления результатов в канале будем тут же асинхронно от остальных результатов посылать их на сайт
				case str := <-resultsFromSshServer:
					println(str)
					fmt.Fprint(writer, fmt.Sprintf(`<th colspan="2">%v </th>`, formatStatText(str)))
					//results = append(results, str)
				case <-timeout:
					fmt.Fprint(writer, fmt.Sprintf(`<th colspan="2">%v </th>`, "SSH server response timed out"))
					//results = append(results, "SSH server response timed out")
					return
				}
			}
			fmt.Fprint(writer, `<tr></table>`) //посылаем на сайт конец таблицы
			/*fmt.Println("Results from SSH server:")
			for _, v := range results { //отладка. Выводим ответы от SSH сервера
				println(v)
			}*/
		} else if message != "" {
			fmt.Fprintf(writer, anError, message)
		}
	}
	fmt.Fprint(writer, pageBottom)
}

func processRequest(request *http.Request) (string, bool) {
	if slice, found := request.Form["hosts"]; found && len(slice) > 0 {
		text := slice[0]
		println("received data from client:", text) //отладка. Выводим принятые данные от клиента на сайте
		counter := 0
		counter2 := 1 //1 - записываем поле с адресом, 2 - записываем поле с логином, 3 - записываем поле с паролем
		for _, field := range strings.Fields(text) {
			if counter2 == 2 { //записываем логин хоста с порядковым номером counter
				hosts[counter].login = field
				counter2++
			} else if counter2 == 3 { //записываем пароль хоста с порядковым номером counter
				hosts[counter].password = field
				counter++    //увеличиваем счетчик (записали пароль текущего хоста и переходим к следующему)
				counter2 = 1 //следующее поле будет содержать адрес
			} else { //добавляем новую структуру хоста в слайс хостов и записываем адрес (айпи:порт) хоста с порядковым номером counter
				host := &host{
					addr:     "",
					login:    "",
					password: "",
				}
				hosts = append(hosts, host)
				hosts[counter].addr = field
				counter2++
			}
		}
	} else {
		return "", false //при первом отображении сайта клиенту от него не приходит никаких данных, поэтому не будем высылать ему статистику, пока он не пришлет данные
	}
	return "", true
}

func sshClient(host *host) string {
	config := &ssh.ClientConfig{
		User: host.login,
		Auth: []ssh.AuthMethod{
			ssh.Password(host.password),
		},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
	}
	client, err := ssh.Dial("tcp", host.addr, config)
	if err != nil {
		return err.Error()
	}
	return sendCommandToSshServer(client, host.addr, "df -h") //здесь указываем, какую команду выполнить на серверах (df -h выводит информацию по занимаемой памяти жесткого диска)
}

func sendCommandToSshServer(client *ssh.Client, addr string, command string) string {
	result := "Result for: " + addr + "\n"
	session, err := client.NewSession()
	if err != nil {
		result += err.Error() //ошибку запишем в результат работы (корректно выведится на сайте)
	}
	defer session.Close()
	b, err := session.CombinedOutput(command) //отправляем команду на удаленный ssh сервер и одновременно получаем результат от сервера
	if err != nil {
		result += err.Error()
	}
	result += string(b)
	return result
}

func formatStatText(str string) string {  //форматируем текст под html (\n заменяем на <br>)
	return strings.ReplaceAll(str, "\n", "<br>")
}
