(*
 *  Programming Assignment 1
 *  Stack machine interpreter in Cool.
 *
 *  The stack is a linked list of StackCommand objects. Each kind of
 *  command is a subclass that knows how to display itself and how to
 *  evaluate when it is on top of the stack.
 *)

class StackCommand inherits IO {
   next : StackCommand;

   setNext(n : StackCommand) : StackCommand {
      {
         next <- n;
         self;
      }
   };

   getNext() : StackCommand { next };

   eval(st : Stack) : Object { self };

   getInt() : Int { 0 };

   show() : Object { out_string("?\n") };
};

class IntCmd inherits StackCommand {
   val : Int;

   init(v : Int) : IntCmd {
      {
         val <- v;
         self;
      }
   };

   getInt() : Int { val };

   show() : Object {
      {
         out_string((new A2I).i2a(val));
         out_string("\n");
      }
   };
};

class PlusCmd inherits StackCommand {
   eval(st : Stack) : Object {
      {
         st.pop();
         (let x : Int <- st.pop().getInt() in
            (let y : Int <- st.pop().getInt() in
               st.push((new IntCmd).init(x + y))
            )
         );
      }
   };

   show() : Object { out_string("+\n") };
};

class SwapCmd inherits StackCommand {
   eval(st : Stack) : Object {
      {
         st.pop();
         (let a : StackCommand <- st.pop() in
            (let b : StackCommand <- st.pop() in
               {
                  st.push(a);
                  st.push(b);
               }
            )
         );
      }
   };

   show() : Object { out_string("s\n") };
};

class Stack {
   head : StackCommand;

   isEmpty() : Bool { isvoid head };

   push(c : StackCommand) : Stack {
      {
         c.setNext(head);
         head <- c;
         self;
      }
   };

   pop() : StackCommand {
      (let old : StackCommand <- head in
         {
            head <- head.getNext();
            old;
         }
      )
   };

   dump() : Object {
      (let n : StackCommand <- head in
         while not isvoid n loop
            {
               n.show();
               n <- n.getNext();
            }
         pool
      )
   };

   doEval() : Object {
      if isvoid head then
         self
      else
         head.eval(self)
      fi
   };
};

class Main inherits IO {
   stack : Stack <- new Stack;
   running : Bool <- true;

   main() : Object {
      while running loop
         {
            out_string(">");
            (let cmd : String <- in_string() in
               if cmd = "x" then
                  running <- false
               else
                  if cmd = "d" then
                     stack.dump()
                  else
                     if cmd = "e" then
                        stack.doEval()
                     else
                        if cmd = "+" then
                           stack.push(new PlusCmd)
                        else
                           if cmd = "s" then
                              stack.push(new SwapCmd)
                           else
                              stack.push((new IntCmd).init((new A2I).a2i(cmd)))
                           fi
                        fi
                     fi
                  fi
               fi
            );
         }
      pool
   };
};
