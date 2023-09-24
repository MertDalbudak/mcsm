/*
	This file determines what actions the temp monitor is performing when a specific on board temperature is reached
	
	action['log'] 		->	System log						action['log'][]	->	Multiple system logs
	action['say'] 		->	Server echo to all players
	action['saySync'] 	->	Server echo to all players		action['say'][] ->	Multiple server echos to all players
	action['stop'] 		->	Server shutdown					
		action['stop'][0] -> Time before server shutdown in ms
		action['stop'][1] -> Server shutdown announcement interval in ms
	action['kill']	->	Kill Server
*/

const temperature_levels = [
	{
		"max_temp": 35,
		"action": (...args) => ({
			'log': "Server is running on cool temperature: " + args[0] + " °C"
		})
	},
	{
		"max_temp": 65,
		"action": (...args) => ({
			'log': "Server is running on moderat temperature: " + args[0] + " °C"
		})
	},
	{
		"max_temp": 70,
		"action": (...args) => ({
			'log': "Server is running on warm temperature: " + args[0] + " °C",
			'say': "Running on warm temperature of " + args[0] + " °C"
		})
	},
	{
		"max_temp": 75,
		"action": (...args) => ({
			'log': [
				"Server is running hot on: " + args[0] + " °C", 
				"If the CPU temperature is not below 70 °C Server will shutdown shortly"
			],
			'saySync': [
				"Running on hot temperature of " + args[0] + " °C",
				"If the CPU temperature is not below 70 °C Server will shutdown shortly"
			],
			'stop': [60000, 10000]
		})
	},
	{
		"max_temp": 90,
		"action": (...args) => ({
			'log': ["Server is running very hot on: " + args[0] + " °C", "Server is shutting down in 5 seconds"],
			'say': "Running on very hot temperature of " + args[0] + " °C",
			'stop': [5000, 1000]
		})
	},
	{
		"max_temp": 999,
		"action": (...args) => ({
			'log': "Server is too hot. Server shuting down immediately",
			'kill': true
		})
	}
];

// SORT BEFORE PASSING
module.exports = temperature_levels.sort((a, b) => a.max_temp - b.max_temp);