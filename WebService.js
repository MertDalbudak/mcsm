const net = require('net');
const Event = require('events');

class WebService {
    constructor(manager, port, authentication){
        this.manager = manager;
        this.port = port;
        this.authentication = authentication;
        this.event = new Event();
        this.server = net.createServer((socket)=> { //'connection' listener
            socket.on('data', async (data)=> {
                try{
                    data = JSON.parse(data.toString());
                }catch(error){
                    console.error(error);
                    socket.write(WebService.error_response("Request wasn't a valid json"));
                    WebService.send(socket);
                    return;
                }
                console.log(data);
                if(this.authentication == data.authentication){
                    try{
                        this[data.command.name](socket, data.command.args);
                    }
                    catch{
                        this.notSupported(socket);
                    }
                }
                else{
                    socket.write(WebService.error_response("Authentication failed"));
                    WebService.send(socket);
                }
            });
            socket.on('end', function() {
                console.log('server disconnected');
            });
        });
    }

    listen(port = this.port){
        this.server.listen(port, ()=>{ //'listening' listener
            console.log(`Socket (${port}) is listening`);
        });
    }

    async getTemp(socket, data){
        socket.write(WebService.msg_response(this.manager.recent_system_temp.toString()))
        WebService.send(socket);
    }
    async stopServer(socket, data){
        console.log(data);
        const slot = this.manager.slot;
        if(slot){
            slot.stopServer((error, data)=>{
                if(error == null){
                    socket.write(WebService.msg_response(data.message));
                }
                else{
                    if(data.command.args.id)
                        socket.write(WebService.error_response(error, data.message));
                }
                WebService.send(socket);
            });
        }
        else{
            socket.write(WebService.error_response("No matching slot found"));
            WebService.send(socket);
        }
    }
    async startServer(socket, data){
        console.log(data);
        const slot = this.manager.slot;
        if(slot){
            if(slot.suspend_mc_start){
                socket.write(WebService.error_response("Starting and restarting are temporarily suspended", false));
                WebService.send(socket);
                return;
            }
            socket.write(WebService.msg_response("Server start has been initialized", true));
            WebService.send(socket, {'end': false});
            slot.startServer(data.server_id, (error, data) => {
                if(error == null){
                    socket.write(WebService.msg_response(data.message));
                }
                else{
                    socket.write(WebService.error_response(error, data.message));
                }
                WebService.send(socket);
            });
        }
        else{
            socket.write(WebService.error_response("No matching slot found"));
            WebService.send(socket);
        }
    }
    async getSlotData(socket, data){
        socket.write(WebService.data_response(await this.manager.slot.report()));
        WebService.send(socket);
    }
    async notSupported(socket, data){
        socket.write("Command not supported");
        WebService.send(socket);
    }
}

WebService.data_response = (data, message = "", keep_alive = false) => {
    return JSON.stringify({'error': null, 'data': data, 'message': message, 'keep_alive': keep_alive});
}

WebService.error_response = (error, message = "", keep_alive = false) => {
    return JSON.stringify({'error': error, 'data': null, 'message': message, 'keep_alive': keep_alive});
}

WebService.msg_response = (message, keep_alive = false) => {
    return JSON.stringify({'error': null, 'data': null, 'message': message, 'keep_alive': keep_alive});
}

WebService.send = (socket, options) => {
    socket.pipe(socket, options);
    if(typeof options != 'object' || options.end){
        socket.end();
    }
}

module.exports = WebService;