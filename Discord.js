const { Client, Collection, Events, GatewayIntentBits, REST, Routes, SlashCommandBuilder  } = require('discord.js');
const Event = require('events')

let supported_commands = [
    {
        'name': "help",
        'description': "Lists all available commands",
        'reply': (issuer)=> `Following commands are available:\n${supported_commands.filter(e => e.active).map(e => `> ${e.name}`).join('\r\n')}`,
        'active': false
    },
    {
        'name': "server",
        'description': "Gets server data",
        'reply': (issuer)=> null,
        'active': false
    },
    {
        'name': "list",
        'description': "Lists the online player count and their names",
        'reply': (issuer)=> null,
        'active': false
    },
    {
        'name': "version",
        'description': "Shows the version which the server is currently running at",
        'reply': (issuer)=> `Checking server version...`,
        'active': false
    },
    {
        'name': "temp",
        'description': "Shows the last fetched temperature of the running server",
        'reply': (issuer)=> `Checking server temperature...`,
        'active': false
    },
    {
        'name': "banlist",
        'description': "List all players that have been banned from the Minecraft server",
        'reply': (issuer)=> `Getting list of banned players...`,
        'active': false
    }
];


module.exports = class{
    constructor(config){
        this.config = config;
        this.event = new Event();
        this.commands_obeyed_counter = 0;
        this.guilds = [];
        this.connected = false;

        this.sendQueue = [];

        this.login();
    }

    async login(){
        this.rest = new REST({ version: '10' }).setToken(this.config.token);
        this.client = new Client({
            intents: [
                GatewayIntentBits.Guilds,
                GatewayIntentBits.GuildMessages,
                GatewayIntentBits.MessageContent
            ]
        });
        const login = await this.client.login(this.config.token).catch(error => {
            console.error(error.toString());
        });
        if(!login){
            console.error("\n\nDISCORD SERVICE IS NOT AVAILABLE\n\n");
            return false;
        }
        else{
            console.log("\n\nDISCORD SERVICE AVAILABLE\n\n")
        }
        this.client.commands = new Collection();
        this.client.on('ready', ()=> {
            this.channels = this.config.channels.map(ch => {
                let channel = this.client.channels.cache.get(ch.id);
                if(!this.guilds.find(guild => guild.id == channel.guild.id)){
                    this.guilds.push(channel.guild);
                }
                return channel;
            });
            this.connected = true;
            this.event.emit('ready');

            this.sendQueue.forEach(e => this.send(e.msg, e.index));
        });
        this.registerIntervalId = setInterval(()=>{
            this.rest.put(
                Routes.applicationCommands(this.config.clientId),
                { 
                    'body': this.client.commands.map(c => c.data.toJSON())
                }
            );
            console.log("Discord commands registerd");
        }, this.config.commandRegisterInterval);
        return true;
    }

    /**
     * 
     * @param {String} msg 
     * @param {Number} index 
     * @returns Boolean
     */
    async send(msg, index = null){
        try {
            if(index){
                await this.channels[index].send(msg);
            }
            else{
                for(let i = 0; i < this.channels.length; i++){
                    await this.channels[i].send(msg);
                };
            }
            return true;
        }
        catch(error){
            this.sendQueue.push({'msg': msg, 'index': index});
            console.error(error);
            return false;
        }
    }

    /**
     * 
     * @param {String} command_name
     * @param {Function} callback
     * @returns Void
     */
    async obeyCommand(command_name, callback){
        const command = supported_commands.find(e => e.name === command_name);
        if(command){
            command.active = true;
            const discord_slash_command = {
                'data': new SlashCommandBuilder().setName(command.name).setDescription(command.description),
                'execute': async (interaction, edit_reply) =>{
                    let reply_msg
                    try{
                        reply_msg = await callback();
                    }
                    catch(error){
                        console.error("List hat nicht funktioniert", error);
                        reply_msg = error;
                    }
                    if(edit_reply){
                        interaction.reply(reply_msg);
                    }
                    else{
                        interaction.editReply(reply_msg);
                    }
                }
            };
            this.client.commands.set(discord_slash_command.data.name, discord_slash_command);
            this.client.on(Events.InteractionCreate, async interaction => {
                if(interaction.isChatInputCommand()){
                    if(interaction.commandName == command_name){
                        if(this.channels.find(ch => ch.id == interaction.channel.id)){
                            const slash_command = this.client.commands.get(interaction.commandName);
                            if(slash_command){
                                let command_reply = command.reply(interaction.user.username);
                                if(command_reply != null){
                                    await interaction.reply(command_reply);
                                }
                                try{
                                    if(callback){
                                        slash_command.execute(interaction, command_reply == null);
                                    }
                                }
                                catch(error){
                                    console.error(error);
                                    await interaction.reply({
                                        'content': "There was an error while executing this command!",
                                        'ephemeral': true
                                    });
                                }
                            }
                            else{
                                try {
                                    await interaction.reply({
                                        'content': "This command is not supported",
                                        'ephemeral': true
                                    });
                                }
                                catch(error){
                                    console.error(error);
                                }
                            }
                        }
                        else{
                            setTimeout(async ()=>{
                                try{
                                    await interaction.reply({
                                        'content': "There is no server linked to this channel currently!\nVisit https://mc.dalbudak.de to start your server.",
                                        'ephemeral': true
                                    });
                                }
                                catch(error){
                                    console.error(error);
                                }
                            }, this.config.no_service_timeout);
                        }
                    }
                }
            });
            this.commands_obeyed_counter++;
            if(this.commands_obeyed_counter == 1){
                this.obeyCommand('help');
            }
        }
        else{
            throw new Error('Obey command not supported');
        }
    }

    // OBEY COMMAND WITHOUT SLASH COMMAND SUPPORT
    /**
     * 
     * @param {String} command_name
     * @param {Function} callback
     * @returns Void
     */
    obeyCommandText(command_name, callback){
        const command = supported_commands.find(e => e.name === command_name);
        if(command){
            command.active = true;
            this.client.on('messageCreate', async (message) => {
                if (this.config.channels.find(ch => ch.id == message.channel.id)) {
                    if(message.content === this.config.commandPrefix + command_name){
                        message.react("👍");
                        let command_reply = command.reply(message.author.username);
                        if(command_reply != null){
                            message.channel.send(command_reply);
                        }
                        try{
                            if(callback){
                                let reply_msg = await callback();
                                message.channel.send(reply_msg);
                            }
                        }
                        catch(err){
                            console.error("Couldn't reply to users request", err);
                        }
                    }
                }
            });
            this.commands_obeyed_counter++;
            if(this.commands_obeyed_counter == 1){
                this.obeyCommand('help');
            }
        }
        else{
            throw new Error('Obey command not supported');
        }
    }
    logout(){
        clearInterval(this.registerIntervalId);
        this.client.destroy();
        this.connected = false;
    }
    /**
     * 
     * @param {String} name
     * @param {Function} callback
     * @returns Void
     */
    on(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.on(name, callback);
    }
    /**
     * 
     * @param {String} name
     * @param {Function} callback
     * @returns Void
     */
    once(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.once(name, callback);
    }
    // GETTER
    get commands_obeyed(){
        return supported_commands.reduce((acc, curr) => {
            if(curr.active)
                acc.push(curr.name);
            return acc;
        }, []);
    }
};