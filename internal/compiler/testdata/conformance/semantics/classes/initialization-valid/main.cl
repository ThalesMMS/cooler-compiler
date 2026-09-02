class Parent inherits IO {
  number : Int;
  text : String;
  flag : Bool;
  reference : Object;
  p1 : Object <- out_string("p1");
  p2 : Object <- out_string("p2");
};

class Child inherits Parent {
  c1 : Object <- out_string("c1");
  c2 : Object <- out_string("c2");
};

class Main inherits Child {
  main() : Object {{
    out_int(number);
    out_string(text);
    if flag then out_string("true") else out_string("false") fi;
    if isvoid reference then out_string("void") else out_string("object") fi;
  }};
};
