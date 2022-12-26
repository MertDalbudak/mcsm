const ServerMonitor = require(__dirname + "/ServerMonitor.js");

const app = new ServerMonitor();

// HEARTBEAT OF CHECKING THE VITAL PARAMETERS OF THE SERVER
const check_interval = 15000; // time in ms


function main(){
	// CHECK IF NEW PAPER VERSION IS AVAILABLE
	//app.update();

	// MONITOR CPU TEMPERATURE
	app.temperature(check_interval);

	// MONITOR CPU FREQUENCY
	//app.frequency(check_interval);

	// TIME INTERVAL OF SERVER BEING RESTARTET
	//app.restartCron('*/1 * * * *');
	// app.restartCron();

	// BAN FLYING PLAYERS
	app.banFlying();

	app.checkKill();

	app.Discord.on('ready',()=> {
		app.discord_obey_command_temp();
		app.discord_obey_command_list();
		app.discord_obey_command_version();
		// app.discord_obey_command_banlist();
	});
}

main();
