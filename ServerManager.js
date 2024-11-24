const fs = require('fs');
const {execSync} = require('child_process');
const Event = require('events');
const net = require('net');
const Slot = require('./Slot');
const WebService = require('./WebService');
const TempPolicy = require('./temp_policy.js');


const config = require('./config.json');

// GLOBAL
const DIR_NAME = __dirname;


module.exports = class {
    constructor(server_id){
        this.monitor = {};
        this.temp_policy = TempPolicy;
        this.shutdown_temp_threshold = null;
        this.recent_system_temp = 0.00;
        this.recent_system_freq = 0;
        this.event = new Event();
        this.slot = new Slot(server_id);
        if(config.WebAPI){
            this.webService();
        }
        else{
            process.stdin.resume();
        }
        setTimeout(()=>this.event.emit('ready'), 100);
    }
    
    // MONITORING
    async temperature(interval_time){
        this.monitor['temperatur'] = setInterval(()=> {
            if(this.slot.server){
                fs.readFile(config.CPUTemperature, (err, data)=>{
                    if(err == null){
                        const temp = (data/1000).toFixed(2);
                        this.recent_system_temp = temp;
                        if(isNaN(this.shutdown_temp_threshold) == false && temp < this.shutdown_temp_threshold){
                            this.slot.server.abortShutdown();
                        }
                        for(let i = 0; i < this.temp_policy.length; i++){
                            if(temp <= this.temp_policy[i]['max_temp']){
                                this.shutdown_temp_threshold = i > 0 ? 0 : this.temp_policy[i - 1]['max_temp'];
                                let actions = this.temp_policy[i]["action"](temp);
                                for(let action in actions){
                                    if(Array.isArray(actions[action]))
                                        this.slot.server.handler[action](...actions[action]);
                                    else
                                        this.slot.server.handler[action](actions[action]);
                                }
                                break;
                            }
                        }
                    }
                    else{
                        console.error(err);
                        clearInterval(this.monitor['temperatur']);
                    }
                });
            }
        }, interval_time);
    }
    async frequency(interval_time){
        this.monitor['frequency'] = setInterval(()=> {
            if(this.slot.server){
                fs.readFile(config.CPUFrequency, {encoding: 'utf-8'}, (err, data)=>{
                    if(err == null){
                        let freq = parseFloat(data.trim()) / 1000 / 1000;
                        this.recent_system_freq = freq;
                        this.slot.server.handler.say(`CPU frequency is ${freq.toFixed(2)} Ghz`);
                    }
                    else{
                        console.error(err);
                        clearInterval(this.monitor['frequency']);
                    }
                });
            }
        }, interval_time);
    }
    async webService(){
        this.web_service = new WebService(this, config.MCSM_API_PORT, config.MCSM_API_AUTHENTICATION);

        this.web_service.listen();
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
};