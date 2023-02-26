const fs = require('fs');
const {execSync} = require('child_process');
const Event = require('events');
const net = require('net');
const Slot = require('./Slot');


const config = require('./config.json');

// GLOBAL
const DIR_NAME = __dirname;


module.exports = class {
    constructor(){
        this.monitor = {};
        this.temp_policy = require('./temp_policy.js');
        this.shutdown_temp_threshold = null;
        this.recent_system_temp = 0.00;
        this.recent_system_freq = 0;
        this.event = new Event();
        this.Slots = config.Slots.map(slot => new Slot(slot.id));
        setTimeout(()=>this.event.emit('ready'), 100);
    }
    
    // MONITORING
    async temperature(interval_time){
        this.monitor['temperatur'] = setInterval(()=> {
            fs.readFile(config.CPUTemperature, (err, data)=>{
                if(err == null){
                    const temp = (data/1000).toFixed(2);
                    this.recent_system_temp = temp;
                    this.active_slots.forEach(slot => {
                        if(isNaN(this.shutdown_temp_threshold) == false && temp < this.shutdown_temp_threshold){
                            slot.Server.abortShutdown();
                        }
                        for(let i = 0; i < this.temp_policy.length; i++){
                            if(temp <= this.temp_policy[i]['max_temp']){
                                this.shutdown_temp_threshold = i > 0 ? 0 : this.temp_policy[i - 1]['max_temp'];
                                let actions = this.temp_policy[i]["action"](temp);
                                for(let action in actions){
                                    if(Array.isArray(actions[action]))
                                        slot.Server.handler[action](...actions[action]);
                                    else
                                        slot.Server.handler[action](actions[action]);
                                }
                                break;
                            }
                        }
                    });
                }
                else{
                    console.error(err);
                    clearInterval(this.monitor['temperatur']);
                }
            });
        }, interval_time);
    }
    async frequency(interval_time){
        this.monitor['frequency'] = setInterval(()=> {
            fs.readFile(config.CPUFrequency, {encoding: 'utf-8'}, (err, data)=>{
                if(err == null){
                    let freq = parseFloat(data.trim()) / 1000 / 1000;
                    this.recent_system_freq = freq;
                    this.handler.say(`CPU frequency is ${freq} Ghz`);
                }
                else{
                    console.error(err);
                    clearInterval(this.monitor['frequency']);
                }
            });
        }, interval_time);
    }
    async webService(){
        // OPEN SOCKET INTERFACE FOR A WEBSERVICE
        var server = net.createServer((socket)=> { //'connection' listener
            console.log('server connected');
            socket.on('data', async (data)=> {
                try{
                    data = JSON.parse(data.toString());
                }catch(error){
                    console.error("Request wasn't a valid json");
                }
                console.log(data);
                if(data.authentication){
                    switch(data.command.name){
                        case "getTemp":
                            socket.write(this.mcsw_msg_response(this.recent_system_temp.toString()))
                            socket.pipe(socket);
                            break;
                        case "stopServer":
                            this.stopServer((error, data)=>{
                                if(error == null){
                                    socket.write(this.mcsw_msg_response(data.message));
                                }
                                else{
                                    if(data.command.args.id)
                                        socket.write(this.mcsw_error_response(error, data.message));
                                }
                                socket.pipe(socket);
                            });
                            break;
                        case "startServer":
                            if(this.suspend_mc_start){
                                socket.write(this.mcsw_error_response("Starting and restarting are temporarily suspended", false));
                                socket.pipe(socket);
                                return;
                            }
                            socket.write(this.mcsw_msg_response("Server start has been initialized", true));
                            socket.pipe(socket, {'end': false});
                            this.startServer(data.command.args.id, (error, data) => {
                                if(error == null){
                                    socket.write(this.mcsw_msg_response(data.message));
                                }
                                else{
                                    socket.write(this.mcsw_error_response(error, data.message));
                                }
                                socket.pipe(socket);
                            });
                            break;
                        case "getSlotList":
                            socket.write(this.mcsw_data_response(await Promise.all(this.Slots.map(async e => await e.report())), "all good"));
                            socket.pipe(socket);
                            break;
                        default:
                            socket.write("Command not supported");
                            socket.pipe(socket);
                    }
                }
                else{
                    socket.write("Authentication failed");
                    socket.pipe(socket);
                }
            });
            socket.on('end', function() {
                console.log('server disconnected');
            });
        });
        server.listen(config.MCSM_API_PORT, function() { //'listening' listener
            console.log(`Socket (${config.MCSM_API_PORT}) is listening`);
        });
    }
    
    
    

    mcsw_data_response(data, message, keep_alive = false){
        return JSON.stringify({'error': null, 'data': data, 'message': message, 'keep_alive': keep_alive});
    }

    mcsw_error_response(error, message, keep_alive = false){
        return JSON.stringify({'error': error, 'message': message, 'keep_alive': keep_alive});
    }

    mcsw_msg_response(message, keep_alive = false){
        return JSON.stringify({'error': null, 'message': message, 'keep_alive': keep_alive});
    }

    on(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.on(name, callback);
    }
    once(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.once(name, callback);
    }

    // GETTER
    get server_id(){
        return this.mc_server.id;
    }
    get node_version() {
        return execSync("node --version").toString().trim();
    }
    get active_slots(){
        return this.Slots.filter(slot => (slot.status == 'running'));
    }
};