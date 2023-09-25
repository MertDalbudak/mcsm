process.env.ROOT = __dirname;
//const ServerMonitor = require(__dirname + "/ServerMonitor.js");
const ServerManager = require(__dirname + "/ServerManager.js");
const args = process.argv.slice(2);
const server_id = args[0];

//const app = new ServerMonitor(server_id);
const app = new ServerManager(server_id);

// HEARTBEAT OF CHECKING THE VITAL PARAMETERS OF THE SERVER
const check_interval = 15000; // time in ms


function main(){
	// CHECK IF NEW PAPER VERSION IS AVAILABLE
	//app.update();

	// MONITOR CPU TEMPERATURE
	app.temperature(check_interval);

	// MONITOR CPU FREQUENCY
	app.frequency(check_interval);

	// TIME INTERVAL OF SERVER BEING RESTARTET
	// app.restartCron('*/1 * * * *');
	// app.restartCron();

	// BAN FLYING PLAYERS
}

app.on('ready', ()=> main());
